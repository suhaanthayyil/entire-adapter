from __future__ import annotations

import json
import uuid

from entire_adapter.adapter import EntireCallbackHandler


def test_langchain_handler_writes_transcript_and_emits_hooks(tmp_path):
    handler = EntireCallbackHandler(
        agent_label="unit",
        session_id="langgraph-unit-session",
        repo_path=str(tmp_path),
    )

    calls = []

    def fake_emit(hook_name, payload):
        calls.append((hook_name, payload))

    handler.bridge.client.emit_hook = fake_emit  # type: ignore[method-assign]
    handler.bridge.transcript.path = tmp_path / "session.jsonl"

    root_run = uuid.uuid4()
    tool_run = uuid.uuid4()
    handler.on_chain_start({}, {"prompt": "Change src/app.py"}, run_id=root_run)
    handler.on_tool_start({"name": "Write"}, "{}", run_id=tool_run, inputs={"file_path": "src/app.py"})
    handler.on_tool_end("ok", run_id=tool_run)
    handler.on_chain_end({"answer": "done"}, run_id=root_run)

    assert [name for name, _ in calls] == [
        "session-start",
        "turn-start",
        "turn-end",
        "session-end",
    ]
    assert calls[0][1]["session_id"] == "langgraph-unit-session"
    lines = [json.loads(line) for line in handler.bridge.transcript.path.read_text().splitlines()]
    assert lines[0]["type"] == "user"
    assert lines[1]["content"][0]["type"] == "tool_use"
    assert lines[2]["content"][0]["result"]["output"] == "ok"

from __future__ import annotations

import json
import uuid

from entire_adapter.adapter import EntireCallbackHandler, EntireCrewAIListener, ToolCheckpointContext


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
    assert calls[0][1]["raw_data"]["entire_agent_name"] == "langgraph"
    assert calls[2][1]["tool_use_id"] == str(tool_run)
    assert calls[2][1]["response_message"] == "ok"
    lines = [json.loads(line) for line in handler.bridge.transcript.path.read_text().splitlines()]
    assert lines[0]["type"] == "user"
    assert lines[0]["agent"] == "langgraph"
    assert lines[1]["content"][0]["type"] == "tool_use"
    assert lines[2]["content"][0]["result"]["output"] == "ok"


def test_framework_defaults_use_distinct_entire_agent_names(tmp_path):
    langgraph = EntireCallbackHandler(session_id="lg", repo_path=str(tmp_path))
    crewai = EntireCrewAIListener(session_id="crew", repo_path=str(tmp_path))

    assert langgraph.bridge.entire_agent_name == "langgraph"
    assert crewai.bridge.entire_agent_name == "crewai"
    assert langgraph.bridge.client.entire_agent_name == "langgraph"
    assert crewai.bridge.client.entire_agent_name == "crewai"


def test_checkpoint_policy_names_control_turn_end(tmp_path):
    policies = {
        "always": True,
        "never": False,
        "on_success": True,
        "on_error": False,
    }

    for policy, expected in policies.items():
        handler = EntireCallbackHandler(
            session_id=f"policy-{policy}",
            repo_path=str(tmp_path),
            checkpoint_policy=policy,
        )
        calls = []
        handler.bridge.client.emit_hook = lambda hook, payload: calls.append((hook, payload))  # type: ignore[method-assign]
        handler.on_tool_start({"name": "Write"}, "{}", run_id=f"start-{policy}")
        handler.on_tool_end("ok", run_id=f"start-{policy}")
        assert ("turn-end" in [name for name, _ in calls]) is expected


def test_checkpoint_policy_mapping_and_callable(tmp_path):
    seen_contexts: list[ToolCheckpointContext] = []

    def only_write(context: ToolCheckpointContext) -> bool:
        seen_contexts.append(context)
        return context.tool_name == "Write"

    handler = EntireCallbackHandler(
        session_id="policy-callable",
        repo_path=str(tmp_path),
        checkpoint_policy={"Read": "never", "Write": only_write},
    )
    calls = []
    handler.bridge.client.emit_hook = lambda hook, payload: calls.append((hook, payload))  # type: ignore[method-assign]

    handler.on_tool_start({"name": "Read"}, "{}", run_id="read")
    handler.on_tool_end("read ok", run_id="read")
    handler.on_tool_start({"name": "Write"}, "{}", run_id="write")
    handler.on_tool_end("write ok", run_id="write")

    turn_end_calls = [payload for name, payload in calls if name == "turn-end"]
    assert len(turn_end_calls) == 1
    assert turn_end_calls[0]["tool_name"] == "Write"
    assert turn_end_calls[0]["raw_data"]["metadata"]["checkpoint_policy"] == "callable"
    assert seen_contexts[0].tool_name == "Write"


def test_legacy_checkpoint_on_tool_end_still_controls_default(tmp_path):
    handler = EntireCallbackHandler(
        session_id="legacy-off",
        repo_path=str(tmp_path),
        checkpoint_on_tool_end=False,
    )
    calls = []
    handler.bridge.client.emit_hook = lambda hook, payload: calls.append((hook, payload))  # type: ignore[method-assign]

    handler.on_tool_start({"name": "Write"}, "{}", run_id="tool")
    handler.on_tool_end("ok", run_id="tool")

    assert [name for name, _ in calls] == ["session-start"]


def test_tool_run_context_is_consumed_after_tool_end(tmp_path):
    handler = EntireCallbackHandler(
        session_id="tool-context-lifecycle",
        repo_path=str(tmp_path),
    )
    calls = []
    handler.bridge.client.emit_hook = lambda hook, payload: calls.append((hook, payload))  # type: ignore[method-assign]

    handler.on_tool_start(
        {"name": "Write"},
        "{}",
        run_id="shared-run",
        inputs={"file_path": "src/app.py"},
    )
    handler.on_tool_end("ok", run_id="shared-run")

    assert handler.bridge._tool_runs == {}

    handler.on_tool_end("late duplicate", run_id="shared-run", tool_name="LateTool")

    turn_ends = [payload for name, payload in calls if name == "turn-end"]
    assert len(turn_ends) == 2
    assert turn_ends[0]["tool_name"] == "Write"
    assert turn_ends[0]["tool_input"] == {"file_path": "src/app.py"}
    assert turn_ends[1]["tool_name"] == "LateTool"
    assert turn_ends[1]["tool_input"] is None

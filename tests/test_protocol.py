from __future__ import annotations

import base64
import io
import json
from pathlib import Path

from entire_adapter import protocol


def run_protocol(args: list[str], data: bytes = b"", *, spec=protocol.GENERIC_SPEC):
    stdout = io.BytesIO()
    stderr = io.StringIO()
    code = protocol.handle(args, io.BytesIO(data), stdout, stderr, spec=spec)
    return code, stdout.getvalue(), stderr.getvalue()


def test_info_declares_external_agent_contract():
    for spec, expected_type in [
        (protocol.GENERIC_SPEC, "Entire Adapter"),
        (protocol.LANGGRAPH_SPEC, "LangGraph"),
        (protocol.CREWAI_SPEC, "CrewAI"),
    ]:
        code, out, err = run_protocol(["info"], spec=spec)
        assert code == 0, err
        payload = json.loads(out)
        assert payload["protocol_version"] == 1
        assert payload["name"] == spec.name
        assert payload["type"] == expected_type
        assert "turn-end" in payload["hook_names"]
        assert payload["capabilities"]["hooks"] is True
        assert payload["capabilities"]["compact_transcript"] is True


def test_parse_hook_turn_start_maps_to_entire_event():
    raw = {
        "session_id": "langgraph-demo-abc123",
        "session_ref": "/tmp/session.jsonl",
        "user_prompt": "Fix the bug",
        "timestamp": "2026-05-19T12:00:00Z",
        "tool_use_id": "tool-1",
        "tool_input": {"file_path": "src/app.py"},
        "task_description": "LangGraph demo run",
        "response_message": "done",
        "raw_data": {
            "framework": "langgraph",
            "agent_label": "demo",
            "display_name": "langgraph:demo",
            "entire_agent_name": "langgraph",
            "metadata": {
                "checkpoint_policy": "always",
                "checkpoint_reason": "unit test",
            },
        },
    }
    code, out, err = run_protocol(
        ["parse-hook", "--hook", "turn-start"],
        json.dumps(raw).encode(),
        spec=protocol.LANGGRAPH_SPEC,
    )
    assert code == 0, err
    payload = json.loads(out)
    assert payload["type"] == 2
    assert payload["session_id"] == "langgraph-demo-abc123"
    assert payload["prompt"] == "Fix the bug"
    assert payload["metadata"]["framework"] == "langgraph"
    assert payload["metadata"]["agent_label"] == "demo"
    assert payload["metadata"]["display_name"] == "langgraph:demo"
    assert payload["metadata"]["entire_agent_name"] == "langgraph"
    assert payload["metadata"]["checkpoint_policy"] == "always"
    assert payload["tool_use_id"] == "tool-1"
    assert payload["tool_input"] == {"file_path": "src/app.py"}
    assert payload["task_description"] == "LangGraph demo run"
    assert payload["response_message"] == "done"


def test_transcript_helpers(tmp_path: Path):
    transcript = tmp_path / "session.jsonl"
    transcript.write_text(
        "\n".join(
            [
                json.dumps({"type": "user", "content": [{"text": "Build it"}]}),
                json.dumps(
                    {
                        "type": "assistant",
                        "content": [
                            {
                                "type": "tool_use",
                                "name": "Write",
                                "input": {"file_path": "src/app.py"},
                                "result": {"output": "ok"},
                            }
                        ],
                    }
                ),
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    code, out, err = run_protocol(["get-transcript-position", "--path", str(transcript)])
    assert code == 0, err
    assert json.loads(out)["position"] == 2

    code, out, err = run_protocol(
        ["extract-modified-files", "--path", str(transcript), "--offset", "0"]
    )
    assert code == 0, err
    assert json.loads(out)["files"] == ["src/app.py"]

    code, out, err = run_protocol(
        ["extract-prompts", "--session-ref", str(transcript), "--offset", "0"]
    )
    assert code == 0, err
    assert json.loads(out)["prompts"] == ["Build it"]

    code, out, err = run_protocol(
        ["compact-transcript", "--session-ref", str(transcript)],
        spec=protocol.LANGGRAPH_SPEC,
    )
    assert code == 0, err
    compacted = base64.b64decode(json.loads(out)["transcript"]).decode()
    assert '"agent":"langgraph"' in compacted
    assert compacted.endswith("\n")


def test_chunk_and_reassemble_roundtrip():
    code, out, err = run_protocol(["chunk-transcript", "--max-size", "5"], b"hello\nworld\n")
    assert code == 0, err
    chunks = json.loads(out)["chunks"]
    assert all(isinstance(chunk, str) for chunk in chunks)

    code, out, err = run_protocol(["reassemble-transcript"], json.dumps({"chunks": chunks}).encode())
    assert code == 0, err
    assert out == b"hello\nworld\n"

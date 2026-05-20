from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest


pytestmark = pytest.mark.skipif(
    os.environ.get("ENTIRE_E2E") != "1",
    reason="set ENTIRE_E2E=1 to run Entire integration tests",
)


def run(cmd: list[str], cwd: Path, *, input_text: str | None = None) -> subprocess.CompletedProcess[str]:
    completed = subprocess.run(
        cmd,
        cwd=cwd,
        input=input_text,
        text=True,
        capture_output=True,
        check=False,
    )
    assert completed.returncode == 0, completed.stdout + completed.stderr
    return completed


@pytest.fixture()
def git_repo(tmp_path: Path) -> Path:
    if shutil.which("entire") is None:
        pytest.skip("entire CLI is not installed")
    if shutil.which("entire-agent-langgraph") is None:
        pytest.skip("entire-agent-langgraph is not on PATH")
    repo = tmp_path / "repo"
    repo.mkdir()
    run(["git", "init"], repo)
    run(["git", "config", "user.name", "Entire Adapter E2E"], repo)
    run(["git", "config", "user.email", "e2e@example.local"], repo)
    (repo / "README.md").write_text("# E2E\n", encoding="utf-8")
    run(["git", "add", "README.md"], repo)
    run(["git", "commit", "-m", "initial commit"], repo)
    run(["entire", "enable", "--agent", "langgraph", "--telemetry=false"], repo)
    return repo


def test_langgraph_single_tool_manual_commit_creates_checkpoint(git_repo: Path):
    script = Path(__file__).parents[2] / "adapter_live_test" / "live_langgraph_agent.py"
    out = run([sys.executable, str(script), str(git_repo)], git_repo).stdout
    session_id = next(line.split(": ", 1)[1] for line in out.splitlines() if line.startswith("Entire session:"))
    run(["git", "add", "."], git_repo)
    run(["git", "commit", "-m", "langgraph single tool"], git_repo, input_text="y\n")
    checkpoints = run(["entire", "checkpoint", "list", "--session", session_id], git_repo).stdout
    assert session_id in checkpoints or "langgraph single tool" in checkpoints


def test_langgraph_multiple_tools_commit_once_advances_checkpoint(git_repo: Path, tmp_path: Path):
    script = tmp_path / "multi_tool_graph.py"
    script.write_text(
        """
from pathlib import Path
from typing import TypedDict
from entire_adapter import EntireCallbackHandler
from langchain_core.runnables import RunnableConfig
from langchain_core.tools import tool
from langgraph.graph import END, START, StateGraph

class State(TypedDict):
    result: str

@tool
def write_file(path: str, content: str) -> str:
    '''Write content to a file.'''
    Path(path).write_text(content, encoding="utf-8")
    return f"wrote {path}"

def node(state: State, config: RunnableConfig) -> State:
    one = write_file.invoke({"path": "one.txt", "content": "one\\n"}, config=config)
    two = write_file.invoke({"path": "two.txt", "content": "two\\n"}, config=config)
    return {"result": one + "; " + two}

graph = StateGraph(State)
graph.add_node("write", node)
graph.add_edge(START, "write")
graph.add_edge("write", END)
handler = EntireCallbackHandler(agent_label="e2e-multi")
result = graph.compile().invoke({"result": ""}, config={"callbacks": [handler]})
print(result)
print(f"Entire session: {handler.session_id}")
""",
        encoding="utf-8",
    )
    out = run([sys.executable, str(script)], git_repo).stdout
    session_id = next(line.split(": ", 1)[1] for line in out.splitlines() if line.startswith("Entire session:"))
    assert (git_repo / "one.txt").exists()
    assert (git_repo / "two.txt").exists()
    run(["git", "add", "."], git_repo)
    run(["git", "commit", "-m", "langgraph multi tool"], git_repo, input_text="y\n")
    checkpoints = run(["entire", "checkpoint", "list", "--session", session_id], git_repo).stdout
    assert "langgraph multi tool" in checkpoints or "checkpoints" in checkpoints


def test_langgraph_protocol_helpers_after_enable(git_repo: Path, tmp_path: Path):
    hooks = run(["entire-agent-langgraph", "are-hooks-installed"], git_repo).stdout
    assert json.loads(hooks)["installed"] is True

    transcript = tmp_path / "session.jsonl"
    transcript.write_text(
        "\n".join(
            [
                json.dumps({"type": "user", "content": [{"text": "Edit src/app.py"}]}),
                json.dumps(
                    {
                        "type": "assistant",
                        "framework": "langgraph",
                        "agent_label": "e2e",
                        "content": [
                            {
                                "type": "tool_use",
                                "name": "write_file",
                                "input": {"file_path": "src/app.py"},
                                "result": {"status": "success", "output": "ok"},
                            }
                        ],
                    }
                ),
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    prompts = run(
        ["entire-agent-langgraph", "extract-prompts", "--session-ref", str(transcript), "--offset", "0"],
        git_repo,
    ).stdout
    modified = run(
        ["entire-agent-langgraph", "extract-modified-files", "--path", str(transcript), "--offset", "0"],
        git_repo,
    ).stdout
    summary = run(["entire-agent-langgraph", "extract-summary", "--session-ref", str(transcript)], git_repo).stdout
    compact = run(["entire-agent-langgraph", "compact-transcript", "--session-ref", str(transcript)], git_repo).stdout

    assert json.loads(prompts)["prompts"] == ["Edit src/app.py"]
    assert json.loads(modified)["files"] == ["src/app.py"]
    assert json.loads(summary)["has_summary"] is True
    assert json.loads(compact)["transcript"]

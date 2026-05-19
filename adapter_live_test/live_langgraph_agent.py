"""Live LangGraph smoke test for entire-adapter.

This script is run by run_live_test.sh inside a temporary Git repo.
It writes a file through a LangChain tool so EntireCallbackHandler receives
on_tool_start/on_tool_end events and can trigger an Entire turn-end hook.
"""

from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import TypedDict

from entire_adapter import EntireCallbackHandler
from langchain_core.runnables import RunnableConfig
from langchain_core.tools import tool
from langgraph.graph import END, START, StateGraph


class AgentState(TypedDict):
    prompt: str
    output_path: str
    result: str


@tool
def write_agent_file(text: str, output_path: str = "agent_output.txt") -> str:
    """Write agent output to a file in the current Git repo."""
    path = Path(output_path)
    path.write_text(
        "Entire adapter live test\n"
        f"Prompt: {text}\n"
        "This file was written by a LangChain tool inside a LangGraph run.\n",
        encoding="utf-8",
    )
    return f"wrote {path}"


def write_node(state: AgentState, config: RunnableConfig) -> AgentState:
    result = write_agent_file.invoke(
        {"text": state["prompt"], "output_path": state["output_path"]},
        config=config,
    )
    return {
        "prompt": state["prompt"],
        "output_path": state["output_path"],
        "result": result,
    }


def build_graph():
    graph = StateGraph(AgentState)
    graph.add_node("write_file", write_node)
    graph.add_edge(START, "write_file")
    graph.add_edge("write_file", END)
    return graph.compile()


def main() -> int:
    repo_path = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path.cwd()
    os.chdir(repo_path)

    handler = EntireCallbackHandler(
        agent_label="live-test",
        repo_path=repo_path,
        model="langgraph-live-test",
    )

    app = build_graph()
    result = app.invoke(
        {
            "prompt": "Create a visible file change for Entire checkpoint testing.",
            "output_path": "agent_output.txt",
        },
        config={"callbacks": [handler]},
    )

    print(f"Graph result: {result}")
    print(f"Entire session: {handler.session_id}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

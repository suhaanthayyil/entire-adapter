"""LangGraph repo-editing example for Entire Adapter.

Run inside an Entire-enabled Git repo:

    entire enable --agent langgraph --telemetry=false
    python examples/langgraph_repo_editor.py
"""

from __future__ import annotations

from pathlib import Path
from typing import TypedDict

from entire_adapter import EntireCallbackHandler


class EditState(TypedDict):
    path: str
    old: str
    new: str
    result: str


def _tools():
    from langchain_core.runnables import RunnableConfig
    from langchain_core.tools import tool

    @tool
    def read_file(path: str) -> str:
        """Read a UTF-8 text file."""
        return Path(path).read_text(encoding="utf-8")

    @tool
    def write_file(path: str, content: str) -> str:
        """Write a UTF-8 text file."""
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        Path(path).write_text(content, encoding="utf-8")
        return f"wrote {path}"

    @tool
    def replace_text(path: str, old: str, new: str) -> str:
        """Replace text in a UTF-8 file."""
        target = Path(path)
        text = target.read_text(encoding="utf-8")
        if old not in text:
            raise ValueError(f"{old!r} was not found in {path}")
        target.write_text(text.replace(old, new), encoding="utf-8")
        return f"updated {path}"

    def edit_node(state: EditState, config: RunnableConfig) -> EditState:
        path = state["path"]
        if not Path(path).exists():
            write_file.invoke({"path": path, "content": state["old"]}, config=config)
        read_file.invoke({"path": path}, config=config)
        result = replace_text.invoke(
            {"path": path, "old": state["old"], "new": state["new"]},
            config=config,
        )
        return {**state, "result": result}

    return edit_node


def build_graph():
    from langgraph.graph import END, START, StateGraph

    graph = StateGraph(EditState)
    graph.add_node("edit", _tools())
    graph.add_edge(START, "edit")
    graph.add_edge("edit", END)
    return graph.compile()


def main() -> None:
    handler = EntireCallbackHandler(agent_label="repo-editor")
    app = build_graph()
    result = app.invoke(
        {
            "path": "demo_repo_edit.txt",
            "old": "hello from Entire Adapter\n",
            "new": "hello from LangGraph + Entire\n",
            "result": "",
        },
        config={"callbacks": [handler]},
    )
    print(result)
    print(f"Entire session: {handler.session_id}")


if __name__ == "__main__":
    main()

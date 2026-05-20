"""Per-tool checkpoint policy example for LangGraph."""

from __future__ import annotations

from pathlib import Path
from typing import TypedDict

from entire_adapter import EntireCallbackHandler, ToolCheckpointContext


class PolicyState(TypedDict):
    path: str
    result: str


def checkpoint_large_writes(context: ToolCheckpointContext) -> bool:
    return context.tool_name == "write_file" and len(context.output_summary) < 2000


def build_graph():
    from langchain_core.runnables import RunnableConfig
    from langchain_core.tools import tool
    from langgraph.graph import END, START, StateGraph

    @tool
    def read_file(path: str) -> str:
        """Read a file without creating a checkpoint."""
        return Path(path).read_text(encoding="utf-8") if Path(path).exists() else ""

    @tool
    def write_file(path: str, content: str) -> str:
        """Write a file and create a checkpoint if the callable allows it."""
        Path(path).write_text(content, encoding="utf-8")
        return f"wrote {path}"

    def node(state: PolicyState, config: RunnableConfig) -> PolicyState:
        read_file.invoke({"path": state["path"]}, config=config)
        result = write_file.invoke(
            {"path": state["path"], "content": "policy controlled checkpoint\n"},
            config=config,
        )
        return {**state, "result": result}

    graph = StateGraph(PolicyState)
    graph.add_node("policy_demo", node)
    graph.add_edge(START, "policy_demo")
    graph.add_edge("policy_demo", END)
    return graph.compile()


def main() -> None:
    handler = EntireCallbackHandler(
        agent_label="policy-demo",
        checkpoint_policy={
            "read_file": "never",
            "write_file": checkpoint_large_writes,
        },
    )
    result = build_graph().invoke(
        {"path": "policy_output.txt", "result": ""},
        config={"callbacks": [handler]},
    )
    print(result)
    print(f"Entire session: {handler.session_id}")


if __name__ == "__main__":
    main()

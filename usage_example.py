"""Minimal LangGraph usage example for Entire Adapter.

Run setup once from a git repository:

    pip install -e .
    entire enable --agent entire-adapter --telemetry=false

Then pass the callback in the graph invoke config.
"""

from __future__ import annotations

from typing import TypedDict

from entire_adapter import EntireCallbackHandler


class AgentState(TypedDict):
    prompt: str
    answer: str


def answer_node(state: AgentState) -> AgentState:
    # Replace this with your actual model/tool pipeline.
    return {"prompt": state["prompt"], "answer": f"Processed: {state['prompt']}"}


def build_graph():
    from langgraph.graph import END, START, StateGraph

    graph = StateGraph(AgentState)
    graph.add_node("answer", answer_node)
    graph.add_edge(START, "answer")
    graph.add_edge("answer", END)
    return graph.compile()


if __name__ == "__main__":
    app = build_graph()
    entire = EntireCallbackHandler(agent_label="demo-langgraph")
    result = app.invoke(
        {"prompt": "Update the project safely"},
        config={"callbacks": [entire]},
    )
    print(result)
    print(f"Entire session: {entire.session_id}")

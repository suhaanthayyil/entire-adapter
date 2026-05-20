"""CrewAI multi-agent example for Entire Adapter.

This example is import-safe when CrewAI is not installed. Running it requires
CrewAI plus whatever LLM configuration your CrewAI environment normally uses.
"""

from __future__ import annotations

from entire_adapter import EntireCrewAIListener


def build_crew():
    try:
        from crewai import Agent, Crew, Process, Task
    except ImportError as exc:  # pragma: no cover - optional example dependency.
        raise SystemExit("Install with `pip install entire-adapter[crewai]` first.") from exc

    researcher = Agent(
        role="Repository Researcher",
        goal="Inspect the repository and identify the safest file update.",
        backstory="You are careful, concise, and prefer small diffs.",
    )
    editor = Agent(
        role="Repository Editor",
        goal="Apply the requested repository update.",
        backstory="You make minimal, reviewable changes.",
    )
    research_task = Task(
        description="Find the most appropriate file for: {request}",
        expected_output="A short file/change recommendation.",
        agent=researcher,
    )
    edit_task = Task(
        description="Apply the recommended edit for: {request}",
        expected_output="A summary of the change made.",
        agent=editor,
        context=[research_task],
    )
    return Crew(
        agents=[researcher, editor],
        tasks=[research_task, edit_task],
        process=Process.sequential,
    )


def main() -> None:
    entire_listener = EntireCrewAIListener(agent_label="repo-crew")
    crew = build_crew()
    result = crew.kickoff(inputs={"request": "Add a short note to the project README."})
    entire_listener.close()
    print(result)
    print(f"Entire session: {entire_listener.session_id}")


if __name__ == "__main__":
    main()

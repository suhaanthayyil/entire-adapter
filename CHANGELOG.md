# Changelog

## 0.2.2 - 2026-06-03

Reliability patch.

- Clear completed tool-run context after `on_tool_end` and `on_tool_error` handling so long-running agents do not retain stale tool metadata.
- Add regression coverage for duplicate or late tool-end events to ensure stale file/path inputs cannot leak into later checkpoints.

## 0.2.1 - 2026-05-19

Documentation patch.

- Avoid angle-bracket placeholders in README/docs so PyPI renders protocol and transcript paths correctly.

## 0.2.0 - 2026-05-19

Framework-aware release.

- Add `entire-agent-langgraph` and `entire-agent-crewai` external-agent binaries.
- Keep `entire-agent-entire-adapter` for backward-compatible generic use.
- Add richer dashboard metadata including framework, agent label, display name, run IDs, tool IDs, and checkpoint policy reasons.
- Add per-tool checkpoint policies with named and callable policies.
- Add opt-in async hook dispatch for high-volume agents.
- Add real LangGraph repo-editing examples, a CrewAI multi-agent example, and gated Entire e2e tests.

## 0.1.0 - 2026-05-19

Initial MVP release.

- Add LangChain/LangGraph `EntireCallbackHandler`.
- Add CrewAI `EntireCrewAIListener`.
- Add `entire-agent-entire-adapter` external-agent protocol executable.
- Add transcript persistence, hook parsing, transcript chunking, and compaction.
- Add graceful degradation when Entire is missing or hook execution fails.
- Add unit tests and a manual live LangGraph checkpoint test.

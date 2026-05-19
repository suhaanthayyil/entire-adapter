# Changelog

## 0.1.0 - 2026-05-19

Initial MVP release.

- Add LangChain/LangGraph `EntireCallbackHandler`.
- Add CrewAI `EntireCrewAIListener`.
- Add `entire-agent-entire-adapter` external-agent protocol executable.
- Add transcript persistence, hook parsing, transcript chunking, and compaction.
- Add graceful degradation when Entire is missing or hook execution fails.
- Add unit tests and a manual live LangGraph checkpoint test.

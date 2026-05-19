# Entire Adapter

Python adapter that lets LangGraph/LangChain and CrewAI agents participate in Entire checkpoint tracking.

The package provides:

- `EntireCallbackHandler` for LangChain and LangGraph callback configs.
- `EntireCrewAIListener` for CrewAI's event bus.
- `entire-agent-entire-adapter`, an Entire external-agent protocol executable discovered as `entire-adapter`.

## Why

Custom Python agents often edit files through tools, but their prompt/tool context is easy to lose once the final Git diff lands. Entire Adapter bridges those frameworks into Entire's lifecycle hooks so commits can be linked to the agent session and checkpoint history.

## Install

For LangGraph/LangChain:

```bash
pip install "entire-adapter[langgraph]"
```

For CrewAI:

```bash
pip install "entire-adapter[crewai]"
```

For local development from this repo:

```bash
pip install -e ".[langgraph,crewai,dev]"
```

Enable the adapter inside a Git repo where Entire is installed:

```bash
entire enable --agent entire-adapter --telemetry=false
entire agent list
```

The adapter does not auto-commit your work. Persistent checkpoint metadata is finalized by Entire's existing Git hooks when you commit.

## LangGraph / LangChain

```python
from entire_adapter import EntireCallbackHandler

entire = EntireCallbackHandler(agent_label="reviewer")

result = graph.invoke(
    {"prompt": "Refactor the parser"},
    config={"callbacks": [entire]},
)

print(entire.session_id)
```

The handler maps:

- top-level `on_chain_start` to `session-start` and `turn-start`
- `on_agent_action` and `on_llm_end` to transcript reasoning/text records
- `on_tool_start` to transcript tool context
- `on_tool_end` and `on_tool_error` to transcript tool result and `turn-end`
- top-level `on_chain_end` and `on_chain_error` to `session-end`

See [usage_example.py](usage_example.py) for a minimal graph example.

## CrewAI

Create and keep a listener instance alive in the module where your crew or flow runs:

```python
from entire_adapter import EntireCrewAIListener

entire_listener = EntireCrewAIListener(agent_label="research-crew")
```

The listener registers handlers for crew kickoff, agent execution, and tool usage events. Tool completions trigger Entire `turn-end` hooks so file changes can become checkpoints.

## External-Agent Protocol

Entire discovers external agents from executables named:

```text
entire-agent-<name>
```

This package installs:

```text
entire-agent-entire-adapter
```

So the Entire agent identity is:

```text
entire-adapter
```

Protocol commands include:

```bash
entire-agent-entire-adapter info
entire-agent-entire-adapter detect
entire-agent-entire-adapter parse-hook --hook turn-end < payload.json
entire-agent-entire-adapter read-transcript --session-ref .git/entire-adapter/sessions/demo.jsonl
entire-agent-entire-adapter compact-transcript --session-ref .git/entire-adapter/sessions/demo.jsonl
```

## Metadata And Reasoning

The adapter writes an append-only JSONL transcript outside the worktree when possible:

```text
.git/entire-adapter/sessions/<session-id>.jsonl
```

Each record includes:

- `framework`
- `agent_label`
- `session_id`
- callback/event metadata
- prompt text
- tool name
- tool input
- tool output or error
- assistant/LLM text when available

Entire reads this transcript through the external-agent protocol so reasoning context can be displayed alongside file changes.

## Agent Identity

Entire sees one external agent:

```text
entire-adapter
```

Distinct user agents are separated by generated session IDs and metadata:

```text
langgraph-reviewer-77f263df4f684baf
crewai-research-crew-77f263df4f684baf
```

Use `agent_label` to identify the specific agent or workflow in your project.

## Graceful Degradation

By default, the adapter never crashes your agent if Entire is unavailable. It logs one warning and continues when:

- `entire` is not installed or not on `PATH`
- the current directory is not an Entire-enabled Git repo
- an Entire hook command exits non-zero
- a hook call times out

Use `strict=True` if you want those failures to raise:

```python
EntireCallbackHandler(strict=True)
EntireCrewAIListener(strict=True)
```

## Verification

Run tests:

```bash
python -m pytest -q
```

Build and check the package:

```bash
python -m build
python -m twine check dist/*
```

Manual live checkpoint test commands are in [adapter_live_test/README.md](adapter_live_test/README.md).

After running an agent that edits files:

```bash
git status
git add .
git commit -m "test entire adapter"
entire checkpoint list --session "$SESSION_ID"
```

Use the real session ID printed by the adapter, not an angle-bracket placeholder.

## Live MVP Result

A local live test validated the full path:

```text
LangGraph callback -> Entire Adapter -> Entire hook -> Git commit linkage -> Entire checkpoint list
```

The successful checkpoint output included:

```text
branch       master
session      langgraph-live-test-77f263df4f684baf
checkpoints  1

● e23a3f26c8c4  "Create a visible file change for Entire checkpoint testing."
```

## Development

```bash
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e ".[langgraph,crewai,dev]"
python -m pytest -q
```

Release notes are in [CHANGELOG.md](CHANGELOG.md). Architecture/interview notes are in [docs/INTERVIEW_BRIEF.md](docs/INTERVIEW_BRIEF.md).

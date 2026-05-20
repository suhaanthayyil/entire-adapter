# Entire Adapter

Python adapter that lets LangGraph/LangChain and CrewAI agents participate in Entire checkpoint tracking.

It provides:

- `EntireCallbackHandler` for LangChain and LangGraph callback configs.
- `EntireCrewAIListener` for CrewAI's event bus.
- Three Entire external-agent binaries:
  - `entire-agent-langgraph`
  - `entire-agent-crewai`
  - `entire-agent-entire-adapter` for backward-compatible generic use.

## Why

Custom Python agents often edit files through tools, but their prompt/tool context is easy to lose once the final Git diff lands. Entire Adapter maps framework lifecycle events to Entire hooks so commits can be linked to agent sessions, tool calls, and checkpoint history.

## Install

For LangGraph/LangChain:

```bash
pip install "entire-adapter[langgraph]"
entire enable --agent langgraph --telemetry=false
```

For CrewAI:

```bash
pip install "entire-adapter[crewai]"
entire enable --agent crewai --telemetry=false
```

Generic compatibility mode:

```bash
entire enable --agent entire-adapter --telemetry=false
```

For local development from this repo:

```bash
pip install -e ".[langgraph,crewai,dev]"
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

`EntireCallbackHandler` defaults to Entire agent name `langgraph`. It maps top-level chain start/end, agent actions, LLM output, tool start/end, and tool errors into transcript records and Entire lifecycle hooks.

## CrewAI

```python
from entire_adapter import EntireCrewAIListener

entire_listener = EntireCrewAIListener(agent_label="research-crew")
```

`EntireCrewAIListener` defaults to Entire agent name `crewai`. It listens for crew kickoff, agent execution, and tool usage events. Tool completions trigger `turn-end` so file changes can become checkpoints.

## Checkpoint Policies

Use per-tool policies when not every tool should create a checkpoint:

```python
from entire_adapter import EntireCallbackHandler, ToolCheckpointContext

def checkpoint_writes(context: ToolCheckpointContext) -> bool:
    return context.tool_name == "write_file"

entire = EntireCallbackHandler(
    agent_label="repo-editor",
    checkpoint_policy={
        "read_file": "never",
        "write_file": checkpoint_writes,
    },
)
```

Supported policy names:

```text
always
never
on_success
on_error
```

Mapping by tool name wins first, then a global policy, then the legacy `checkpoint_on_tool_end` flag.

## Async Hook Dispatch

Synchronous hook dispatch remains the default:

```python
EntireCallbackHandler(hook_dispatch="sync")
```

For high-volume agents, opt into a bounded background queue:

```python
entire = EntireCallbackHandler(
    hook_dispatch="async",
    async_queue_size=1024,
    flush_timeout=10.0,
)

# Optional explicit drain at process shutdown.
entire.close()
```

If the async queue fills, non-strict mode warns and skips the hook. `strict=True` raises instead.

## External-Agent Protocol

Entire discovers external agents from executables named:

```text
entire-agent-<name>
```

This package installs:

```text
entire-agent-langgraph
entire-agent-crewai
entire-agent-entire-adapter
```

Protocol commands include:

```bash
entire-agent-langgraph info
entire-agent-crewai info
entire-agent-entire-adapter info
entire-agent-langgraph parse-hook --hook turn-end < payload.json
entire-agent-langgraph read-transcript --session-ref .git/entire-adapter/sessions/demo.jsonl
entire-agent-langgraph compact-transcript --session-ref .git/entire-adapter/sessions/demo.jsonl
```

## Metadata And Dashboard Labels

Each hook/transcript record includes richer labels for dashboards:

- `framework`
- `agent_label`
- `display_name`
- `entire_agent_name`
- `run_id`
- `tool_name`
- `tool_use_id`
- `checkpoint_policy`
- `checkpoint_reason`

The adapter writes append-only JSONL transcripts outside the worktree when possible:

```text
.git/entire-adapter/sessions/<session-id>.jsonl
```

Entire reads this transcript through the external-agent protocol so reasoning and tool context can be displayed alongside file changes.

## Examples

```bash
python usage_example.py
python examples/langgraph_repo_editor.py
python examples/langgraph_checkpoint_policies.py
python examples/crewai_multi_agent.py
```

The CrewAI example requires CrewAI and normal CrewAI LLM configuration.

## Graceful Degradation

By default, the adapter never crashes your agent if Entire is unavailable. It logs one warning and continues when:

- `entire` is not installed or not on `PATH`
- the current directory is not an Entire-enabled Git repo
- an Entire hook command exits non-zero
- a hook call times out
- an async hook queue rejects work in non-strict mode

Use `strict=True` if you want those failures to raise.

## Verification

Run unit tests:

```bash
python -m pytest -q
```

Run gated Entire integration tests when the Entire CLI is available:

```bash
ENTIRE_E2E=1 python -m pytest tests/e2e -q
```

Build and check the package:

```bash
python -m build
python -m twine check dist/*
```

After running an agent that edits files:

```bash
git status
git add .
git commit -m "test entire adapter"
entire checkpoint list --session "$SESSION_ID"
```

Manual live checkpoint commands are in [adapter_live_test/README.md](adapter_live_test/README.md).

## Development

```bash
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e ".[langgraph,crewai,dev]"
python -m pytest -q
```

Release notes are in [CHANGELOG.md](CHANGELOG.md). Architecture/interview notes are in [docs/INTERVIEW_BRIEF.md](docs/INTERVIEW_BRIEF.md).

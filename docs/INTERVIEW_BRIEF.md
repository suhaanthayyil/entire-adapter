# Entire Adapter Interview Brief

## One-line explanation

Entire Adapter lets custom LangGraph/LangChain and CrewAI agents create Entire-compatible lifecycle events, transcripts, and checkpoints, so agent tool steps can be reviewed beside Git file changes.

## What problem it solves

Entire already tracks sessions and checkpoints for supported coding agents. Teams also build custom agents using Python frameworks such as LangGraph and CrewAI. Those custom agents can edit files, call tools, and produce reasoning traces, but without an adapter their steps are not naturally visible in Entire's checkpoint history.

This package bridges that gap.

## Core technologies used

- Python 3.10+ package named `entire-adapter`.
- LangChain/LangGraph callback system through `BaseCallbackHandler`.
- CrewAI event system through `BaseEventListener`.
- Entire external-agent protocol through a console executable named `entire-agent-entire-adapter`.
- Entire CLI hooks through subprocess calls to `entire hooks entire-adapter <hook-name>`.
- Git-backed Entire checkpoint flow, including the `entire/checkpoints/v1` metadata branch created by Entire.
- JSONL transcript files for prompt/tool/reasoning context.
- PyPI packaging through `pyproject.toml`, setuptools, wheel/sdist builds, and `twine`.
- GitHub Actions CI for Python 3.10, 3.11, and 3.12.

## Entire protocol compatibility

Entire discovers external agents by looking for executables on `PATH` named:

```text
entire-agent-<agent-name>
```

This package installs:

```text
entire-agent-entire-adapter
```

So the external agent name is:

```text
entire-adapter
```

The executable implements protocol commands including:

```text
info
detect
get-session-id
get-session-dir
resolve-session-file
read-session
write-session
read-transcript
chunk-transcript
reassemble-transcript
format-resume-command
parse-hook
install-hooks
uninstall-hooks
are-hooks-installed
get-transcript-position
extract-modified-files
extract-prompts
extract-summary
compact-transcript
```

## Lifecycle mapping

LangGraph/LangChain:

```text
top-level on_chain_start -> session-start + turn-start
on_agent_action          -> transcript assistant reasoning record
on_llm_end               -> transcript assistant text record
on_tool_start            -> transcript tool_use record
on_tool_end              -> transcript tool result record + turn-end
on_tool_error            -> transcript tool error record + turn-end
top-level on_chain_end   -> session-end
top-level on_chain_error -> session-end
```

CrewAI:

```text
CrewKickoffStartedEvent       -> session-start + turn-start
AgentExecutionStartedEvent    -> turn-start
AgentExecutionCompletedEvent  -> transcript assistant record
AgentExecutionErrorEvent      -> transcript error record
ToolUsageStartedEvent         -> transcript tool_use record
ToolUsageFinishedEvent        -> transcript tool result record + turn-end
ToolUsageErrorEvent           -> transcript tool error record + turn-end
CrewKickoffCompletedEvent     -> session-end
CrewKickoffFailedEvent        -> session-end
```

## Hook commands used

The adapter calls Entire with these commands:

```bash
entire hooks entire-adapter session-start
entire hooks entire-adapter turn-start
entire hooks entire-adapter turn-end
entire hooks entire-adapter session-end
```

Each command receives JSON on stdin containing:

- `session_id`
- `session_ref`
- `timestamp`
- `user_prompt`
- `tool_name`
- `tool_use_id`
- `tool_input`
- `model`
- `raw_data.framework`
- `raw_data.agent_label`
- `raw_data.metadata`

## Metadata strategy

Entire's normalized event metadata is a string map, so the adapter stores rich metadata in two places:

1. Hook event metadata, compacted to string values for Entire's lifecycle event model.
2. JSONL transcript records, preserving richer prompt/tool/reasoning context.

Transcript location:

```text
.git/entire-adapter/sessions/<session-id>.jsonl
```

If the adapter is not inside a Git repo, it falls back to:

```text
.entire-adapter/sessions/<session-id>.jsonl
```

## Agent identity model

Entire's external agent identity is:

```text
entire-adapter
```

Different user agents are separated with:

- `framework`, for example `langgraph` or `crewai`
- `agent_label`, for example `reviewer` or `research-crew`
- generated `session_id`, for example `langgraph-reviewer-77f263df4f684baf`

This keeps protocol integration simple while still letting dashboards distinguish runs.

## Safety design

The adapter uses graceful degradation.

By default, it never crashes the agent if:

- Entire is not installed.
- `entire` is missing from `PATH`.
- The current folder is not a Git repo.
- Entire hooks are not enabled.
- A hook command exits non-zero.
- A hook call times out.

Failures are logged once with Python `logging`. Developers can opt into strict behavior:

```python
EntireCallbackHandler(strict=True)
EntireCrewAIListener(strict=True)
```

## Live MVP proof

The live test created a sandbox Git repo, enabled Entire, ran a LangGraph tool that wrote a file, committed the file, and confirmed one Entire checkpoint:

```text
Entire: Active Entire Adapter session detected
Last prompt: Create a visible file change for Entire checkpoint testing.

branch       master
session      langgraph-live-test-77f263df4f684baf
checkpoints  1

● e23a3f26c8c4  "Create a visible file change for Entire checkpoint testing."
```

That proves the path:

```text
LangGraph callback -> Entire Adapter -> Entire hook -> Git commit linkage -> Entire checkpoint list
```

## MVP limitations

- The adapter does not auto-commit changes.
- Persistent `entire/checkpoints/v1` metadata is finalized by Entire's existing Git hooks during commit/session workflows.
- v1 uses one external-agent name, `entire-adapter`; per-framework agent names can be added later if the dashboard needs separate top-level identities.
- CrewAI support is event-based and should be tested against the exact CrewAI version used by production users.

## How to explain the value

Entire can now support custom Python agent frameworks, not just prepackaged coding agents. A team can run a LangGraph or CrewAI workflow, let it edit files through tools, and then review both the Git diff and the agent's prompt/tool context in Entire's checkpoint history.

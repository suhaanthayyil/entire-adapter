"""LangChain/LangGraph callbacks and CrewAI listener for Entire."""

from __future__ import annotations

from dataclasses import dataclass
import logging
from typing import Any, Callable, Literal, Mapping

from .utils import (
    AsyncHookDispatcher,
    CREWAI_AGENT_NAME,
    EntireClient,
    LANGGRAPH_AGENT_NAME,
    TranscriptWriter,
    join_text_blocks,
    new_session_id,
    normalize_entire_agent_name,
    safe_text,
    to_jsonable,
    utc_now_iso,
)

try:  # Optional dependency.
    from langchain_core.callbacks import BaseCallbackHandler
except Exception:  # pragma: no cover - exercised only without langchain-core.
    class BaseCallbackHandler:  # type: ignore[no-redef]
        """Fallback so importing this package does not require LangChain."""

        pass


try:  # Optional dependency.
    from crewai.events import (
        AgentExecutionCompletedEvent,
        AgentExecutionErrorEvent,
        AgentExecutionStartedEvent,
        BaseEventListener,
        CrewKickoffCompletedEvent,
        CrewKickoffFailedEvent,
        CrewKickoffStartedEvent,
        ToolUsageErrorEvent,
        ToolUsageFinishedEvent,
        ToolUsageStartedEvent,
    )

    _CREWAI_IMPORT_ERROR: Exception | None = None
except Exception as exc:  # pragma: no cover - depends on optional dependency.
    _CREWAI_IMPORT_ERROR = exc

    class BaseEventListener:  # type: ignore[no-redef]
        def __init__(self) -> None:
            pass

    AgentExecutionCompletedEvent = None  # type: ignore[assignment]
    AgentExecutionErrorEvent = None  # type: ignore[assignment]
    AgentExecutionStartedEvent = None  # type: ignore[assignment]
    CrewKickoffCompletedEvent = None  # type: ignore[assignment]
    CrewKickoffFailedEvent = None  # type: ignore[assignment]
    CrewKickoffStartedEvent = None  # type: ignore[assignment]
    ToolUsageErrorEvent = None  # type: ignore[assignment]
    ToolUsageFinishedEvent = None  # type: ignore[assignment]
    ToolUsageStartedEvent = None  # type: ignore[assignment]


log = logging.getLogger("entire_adapter")

CheckpointPolicyName = Literal["always", "never", "on_success", "on_error"]


@dataclass(frozen=True)
class ToolCheckpointContext:
    """Context passed to callable checkpoint policies."""

    framework: str
    agent_label: str
    session_id: str
    tool_name: str
    tool_input: Any
    output_summary: str
    status: str
    run_id: str
    metadata: Mapping[str, Any]


CheckpointPolicy = CheckpointPolicyName | Callable[[ToolCheckpointContext], bool]
CheckpointPolicyConfig = CheckpointPolicy | Mapping[str, CheckpointPolicy] | None


class _EntireSessionBridge:
    """Common lifecycle bridge used by LangChain and CrewAI integrations."""

    def __init__(
        self,
        *,
        framework: str,
        agent_label: str,
        session_id: str | None,
        repo_path: str | None,
        model: str | None,
        strict: bool,
        checkpoint_on_tool_end: bool = True,
        entire_agent_name: str,
        checkpoint_policy: CheckpointPolicyConfig = None,
        hook_dispatch: Literal["sync", "async"] = "sync",
        async_queue_size: int = 1024,
        flush_timeout: float = 10.0,
        entire_client: EntireClient | None = None,
        transcript: TranscriptWriter | None = None,
    ) -> None:
        self.framework = framework
        self.agent_label = agent_label
        self.entire_agent_name = normalize_entire_agent_name(entire_agent_name)
        self.display_name = format_display_name(framework, agent_label)
        self.session_id = session_id or new_session_id(framework, agent_label)
        self.model = model
        self.checkpoint_on_tool_end = checkpoint_on_tool_end
        self.checkpoint_policy = checkpoint_policy
        self.strict = strict
        self.flush_timeout = flush_timeout
        self.client = entire_client or EntireClient(
            repo_path=repo_path,
            strict=strict,
            entire_agent_name=self.entire_agent_name,
        )
        self.transcript = transcript or TranscriptWriter(
            self.session_id,
            repo_path=self.client.repo_path,
            agent_name=self.entire_agent_name,
        )
        if hook_dispatch not in {"sync", "async"}:
            raise ValueError("hook_dispatch must be 'sync' or 'async'")
        self.dispatcher: AsyncHookDispatcher | None = None
        if hook_dispatch == "async":
            self.dispatcher = AsyncHookDispatcher(
                self.client,
                queue_size=async_queue_size,
                strict=strict,
            )
        self._session_started = False
        self._session_ended = False
        self._tool_runs: dict[str, dict[str, Any]] = {}

    @property
    def session_ref(self) -> str:
        return str(self.transcript.path)

    def start_session(self, metadata: Mapping[str, Any] | None = None) -> None:
        if self._session_started and not self._session_ended:
            return
        self._session_started = True
        self._session_ended = False
        self._emit("session-start", metadata=metadata)

    def start_turn(self, prompt: Any = None, metadata: Mapping[str, Any] | None = None) -> None:
        self.start_session(metadata=metadata)
        prompt_text = extract_prompt(prompt)
        if prompt_text:
            self.transcript.append(
                {
                    "type": "user",
                    "content": [{"type": "text", "text": prompt_text}],
                    "framework": self.framework,
                    "agent_label": self.agent_label,
                    "display_name": self.display_name,
                    "entire_agent_name": self.entire_agent_name,
                    "metadata": to_jsonable(metadata or {}),
                }
            )
        self._emit(
            "turn-start",
            prompt=prompt_text,
            task_description=prompt_text,
            metadata=metadata,
        )

    def assistant_text(self, text: Any, metadata: Mapping[str, Any] | None = None) -> None:
        rendered = safe_text(text)
        if not rendered:
            return
        self.transcript.append(
            {
                "type": "assistant",
                "content": [{"type": "text", "text": rendered}],
                "framework": self.framework,
                "agent_label": self.agent_label,
                "display_name": self.display_name,
                "entire_agent_name": self.entire_agent_name,
                "metadata": to_jsonable(metadata or {}),
            }
        )

    def tool_start(
        self,
        *,
        tool_name: str,
        tool_input: Any = None,
        run_id: Any = None,
        metadata: Mapping[str, Any] | None = None,
    ) -> None:
        self.start_session(metadata=metadata)
        tool_use_id = safe_run_id(run_id)
        self._tool_runs[tool_use_id] = {
            "tool_name": tool_name,
            "tool_input": to_jsonable(tool_input),
            "metadata": to_jsonable(metadata or {}),
        }
        self.transcript.append(
            {
                "type": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "id": tool_use_id,
                        "name": tool_name,
                        "input": to_jsonable(tool_input),
                    }
                ],
                "framework": self.framework,
                "agent_label": self.agent_label,
                "display_name": self.display_name,
                "entire_agent_name": self.entire_agent_name,
                "run_id": tool_use_id,
                "tool_name": tool_name,
                "metadata": to_jsonable(metadata or {}),
            }
        )

    def tool_end(
        self,
        *,
        output: Any = None,
        run_id: Any = None,
        status: str = "success",
        metadata: Mapping[str, Any] | None = None,
    ) -> None:
        tool_use_id = safe_run_id(run_id)
        started = self._tool_runs.get(tool_use_id, {})
        tool_name = str(started.get("tool_name") or (metadata or {}).get("tool_name") or "tool")
        tool_input = started.get("tool_input")
        merged_metadata = dict(started.get("metadata") or {})
        merged_metadata.update(to_jsonable(metadata or {}))
        output_summary = safe_text(output)
        context = ToolCheckpointContext(
            framework=self.framework,
            agent_label=self.agent_label,
            session_id=self.session_id,
            tool_name=tool_name,
            tool_input=tool_input,
            output_summary=output_summary,
            status=status,
            run_id=tool_use_id,
            metadata=merged_metadata,
        )
        should_checkpoint, policy_name, policy_reason = self._should_checkpoint(context)
        merged_metadata.update(
            {
                "checkpoint_policy": policy_name,
                "checkpoint_reason": policy_reason,
            }
        )
        self.transcript.append(
            {
                "type": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "id": tool_use_id,
                        "name": tool_name,
                        "input": tool_input,
                        "result": {
                            "status": status,
                            "output": output_summary,
                        },
                    }
                ],
                "framework": self.framework,
                "agent_label": self.agent_label,
                "display_name": self.display_name,
                "entire_agent_name": self.entire_agent_name,
                "run_id": tool_use_id,
                "tool_name": tool_name,
                "metadata": merged_metadata,
            }
        )
        if should_checkpoint:
            self._emit(
                "turn-end",
                tool_name=tool_name,
                tool_use_id=tool_use_id,
                tool_input=tool_input,
                run_id=tool_use_id,
                checkpoint_policy=policy_name,
                checkpoint_reason=policy_reason,
                task_description=f"{self.display_name} used {tool_name}",
                response_message=output_summary,
                metadata=merged_metadata,
            )

    def end_session(self, metadata: Mapping[str, Any] | None = None) -> None:
        if self._session_ended:
            return
        self.start_session(metadata=metadata)
        self._emit("session-end", metadata=metadata)
        self.flush(self.flush_timeout)
        self._session_ended = True

    def flush(self, timeout: float | None = None) -> bool:
        if self.dispatcher is None:
            return True
        return self.dispatcher.flush(timeout)

    def close(self, timeout: float | None = None) -> None:
        if self.dispatcher is not None:
            self.dispatcher.close(timeout if timeout is not None else self.flush_timeout)

    def _emit(
        self,
        hook_name: str,
        *,
        prompt: str = "",
        tool_name: str = "",
        tool_use_id: str = "",
        tool_input: Any = None,
        run_id: str = "",
        checkpoint_policy: str = "",
        checkpoint_reason: str = "",
        task_description: str = "",
        response_message: str = "",
        metadata: Mapping[str, Any] | None = None,
    ) -> None:
        run_id = run_id or safe_run_id((metadata or {}).get("run_id"))
        enriched_metadata = {
            "framework": self.framework,
            "agent_label": self.agent_label,
            "display_name": self.display_name,
            "entire_agent_name": self.entire_agent_name,
            "run_id": run_id,
            "tool_name": tool_name,
            "tool_use_id": tool_use_id,
            "checkpoint_policy": checkpoint_policy,
            "checkpoint_reason": checkpoint_reason,
            **to_jsonable(metadata or {}),
        }
        raw_data = {
            "framework": self.framework,
            "agent_label": self.agent_label,
            "display_name": self.display_name,
            "entire_agent_name": self.entire_agent_name,
            "metadata": enriched_metadata,
        }
        payload = {
            "hook_type": hook_name,
            "session_id": self.session_id,
            "session_ref": self.session_ref,
            "timestamp": utc_now_iso(),
            "user_prompt": prompt,
            "tool_name": tool_name,
            "tool_use_id": tool_use_id,
            "tool_input": to_jsonable(tool_input),
            "run_id": run_id,
            "display_name": self.display_name,
            "task_description": task_description,
            "response_message": response_message,
            "raw_data": raw_data,
            "model": self.model,
        }
        if self.dispatcher is not None:
            self.dispatcher.emit_hook(hook_name, payload)
        else:
            self.client.emit_hook(hook_name, payload)

    def _should_checkpoint(self, context: ToolCheckpointContext) -> tuple[bool, str, str]:
        policy = self._resolve_checkpoint_policy(context.tool_name)
        if policy is None:
            allowed = bool(self.checkpoint_on_tool_end)
            return (
                allowed,
                f"checkpoint_on_tool_end:{str(allowed).lower()}",
                "legacy checkpoint_on_tool_end setting",
            )
        if callable(policy):
            allowed = bool(policy(context))
            return allowed, "callable", "callable checkpoint policy"
        if policy == "always":
            return True, "always", "tool completed"
        if policy == "never":
            return False, "never", "tool checkpoint disabled"
        if policy == "on_success":
            return context.status == "success", "on_success", f"tool status is {context.status}"
        if policy == "on_error":
            return context.status == "error", "on_error", f"tool status is {context.status}"
        raise ValueError(f"unknown checkpoint policy: {policy!r}")

    def _resolve_checkpoint_policy(self, tool_name: str) -> CheckpointPolicy | None:
        if isinstance(self.checkpoint_policy, Mapping):
            if tool_name in self.checkpoint_policy:
                return self.checkpoint_policy[tool_name]
            if "*" in self.checkpoint_policy:
                return self.checkpoint_policy["*"]
            return None
        return self.checkpoint_policy


class EntireCallbackHandler(BaseCallbackHandler):
    """LangChain/LangGraph callback handler that emits Entire lifecycle hooks."""

    def __init__(
        self,
        agent_label: str = "langgraph",
        session_id: str | None = None,
        repo_path: str | None = None,
        model: str | None = None,
        strict: bool = False,
        checkpoint_on_tool_end: bool = True,
        entire_agent_name: str = LANGGRAPH_AGENT_NAME,
        checkpoint_policy: CheckpointPolicyConfig = None,
        hook_dispatch: Literal["sync", "async"] = "sync",
        async_queue_size: int = 1024,
        flush_timeout: float = 10.0,
    ) -> None:
        super().__init__()
        self.bridge = _EntireSessionBridge(
            framework="langgraph",
            agent_label=agent_label,
            session_id=session_id,
            repo_path=repo_path,
            model=model,
            strict=strict,
            checkpoint_on_tool_end=checkpoint_on_tool_end,
            entire_agent_name=entire_agent_name,
            checkpoint_policy=checkpoint_policy,
            hook_dispatch=hook_dispatch,
            async_queue_size=async_queue_size,
            flush_timeout=flush_timeout,
        )

    @property
    def session_id(self) -> str:
        return self.bridge.session_id

    @property
    def session_ref(self) -> str:
        return self.bridge.session_ref

    def flush(self, timeout: float | None = None) -> bool:
        return self.bridge.flush(timeout)

    def close(self, timeout: float | None = None) -> None:
        self.bridge.close(timeout)

    def on_chain_start(
        self,
        serialized: dict[str, Any],
        inputs: dict[str, Any],
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> Any:
        if parent_run_id is not None:
            return None
        self.bridge.start_turn(
            inputs,
            metadata={
                "event": "on_chain_start",
                "run_id": safe_run_id(run_id),
                "serialized": serialized,
                "tags": tags or [],
                **(metadata or {}),
            },
        )
        return None

    def on_chain_end(
        self,
        outputs: dict[str, Any],
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        **kwargs: Any,
    ) -> Any:
        if parent_run_id is not None:
            return None
        self.bridge.assistant_text(
            outputs,
            metadata={"event": "on_chain_end", "run_id": safe_run_id(run_id)},
        )
        self.bridge.end_session(metadata={"event": "on_chain_end", "run_id": safe_run_id(run_id)})
        return None

    def on_chain_error(
        self,
        error: BaseException,
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        **kwargs: Any,
    ) -> Any:
        if parent_run_id is not None:
            return None
        self.bridge.assistant_text(
            f"Chain error: {error}",
            metadata={"event": "on_chain_error", "run_id": safe_run_id(run_id)},
        )
        self.bridge.end_session(metadata={"event": "on_chain_error", "run_id": safe_run_id(run_id)})
        return None

    def on_tool_start(
        self,
        serialized: dict[str, Any],
        input_str: str,
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        inputs: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> Any:
        tool_name = str(
            kwargs.get("name")
            or serialized.get("name")
            or serialized.get("id")
            or "tool"
        )
        self.bridge.tool_start(
            tool_name=tool_name,
            tool_input=inputs if inputs is not None else input_str,
            run_id=run_id,
            metadata={
                "event": "on_tool_start",
                "parent_run_id": safe_run_id(parent_run_id),
                "tags": tags or [],
                **(metadata or {}),
            },
        )
        return None

    def on_tool_end(
        self,
        output: Any,
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        **kwargs: Any,
    ) -> Any:
        self.bridge.tool_end(
            output=output,
            run_id=run_id,
            metadata={
                "event": "on_tool_end",
                "parent_run_id": safe_run_id(parent_run_id),
                **kwargs,
            },
        )
        return None

    def on_tool_error(
        self,
        error: BaseException,
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        **kwargs: Any,
    ) -> Any:
        self.bridge.tool_end(
            output=f"Tool error: {error}",
            run_id=run_id,
            status="error",
            metadata={
                "event": "on_tool_error",
                "parent_run_id": safe_run_id(parent_run_id),
                **kwargs,
            },
        )
        return None

    def on_agent_action(
        self,
        action: Any,
        *,
        run_id: Any,
        parent_run_id: Any | None = None,
        **kwargs: Any,
    ) -> Any:
        thought = getattr(action, "log", None) or safe_text(action)
        self.bridge.assistant_text(
            thought,
            metadata={
                "event": "on_agent_action",
                "run_id": safe_run_id(run_id),
                "parent_run_id": safe_run_id(parent_run_id),
            },
        )
        return None

    def on_llm_end(self, response: Any, *, run_id: Any, parent_run_id: Any | None = None, **kwargs: Any) -> Any:
        text = extract_llm_text(response)
        if text:
            self.bridge.assistant_text(
                text,
                metadata={
                    "event": "on_llm_end",
                    "run_id": safe_run_id(run_id),
                    "parent_run_id": safe_run_id(parent_run_id),
                },
            )
        return None


class EntireCrewAIListener(BaseEventListener):
    """CrewAI native event listener that emits Entire lifecycle hooks."""

    def __init__(
        self,
        agent_label: str = "crewai",
        session_id: str | None = None,
        repo_path: str | None = None,
        model: str | None = None,
        strict: bool = False,
        checkpoint_on_tool_end: bool = True,
        entire_agent_name: str = CREWAI_AGENT_NAME,
        checkpoint_policy: CheckpointPolicyConfig = None,
        hook_dispatch: Literal["sync", "async"] = "sync",
        async_queue_size: int = 1024,
        flush_timeout: float = 10.0,
    ) -> None:
        super().__init__()
        self.bridge = _EntireSessionBridge(
            framework="crewai",
            agent_label=agent_label,
            session_id=session_id,
            repo_path=repo_path,
            model=model,
            strict=strict,
            checkpoint_on_tool_end=checkpoint_on_tool_end,
            entire_agent_name=entire_agent_name,
            checkpoint_policy=checkpoint_policy,
            hook_dispatch=hook_dispatch,
            async_queue_size=async_queue_size,
            flush_timeout=flush_timeout,
        )
        if _CREWAI_IMPORT_ERROR is not None:
            log.debug("CrewAI listener imported without CrewAI events: %s", _CREWAI_IMPORT_ERROR)

    @property
    def session_id(self) -> str:
        return self.bridge.session_id

    @property
    def session_ref(self) -> str:
        return self.bridge.session_ref

    def flush(self, timeout: float | None = None) -> bool:
        return self.bridge.flush(timeout)

    def close(self, timeout: float | None = None) -> None:
        self.bridge.close(timeout)

    def setup_listeners(self, crewai_event_bus: Any) -> None:
        if CrewKickoffStartedEvent is None:
            return

        @crewai_event_bus.on(CrewKickoffStartedEvent)
        def on_crew_started(source: Any, event: Any) -> None:
            self.bridge.start_turn(
                getattr(event, "inputs", None),
                metadata={
                    "event": getattr(event, "type", "crew_kickoff_started"),
                    "crew_name": getattr(event, "crew_name", None),
                },
            )

        @crewai_event_bus.on(CrewKickoffCompletedEvent)
        def on_crew_completed(source: Any, event: Any) -> None:
            self.bridge.assistant_text(
                getattr(event, "output", None),
                metadata={"event": getattr(event, "type", "crew_kickoff_completed")},
            )
            self.bridge.end_session(metadata={"event": getattr(event, "type", "crew_kickoff_completed")})

        @crewai_event_bus.on(CrewKickoffFailedEvent)
        def on_crew_failed(source: Any, event: Any) -> None:
            self.bridge.assistant_text(
                f"Crew failed: {getattr(event, 'error', '')}",
                metadata={"event": getattr(event, "type", "crew_kickoff_failed")},
            )
            self.bridge.end_session(metadata={"event": getattr(event, "type", "crew_kickoff_failed")})

        @crewai_event_bus.on(AgentExecutionStartedEvent)
        def on_agent_started(source: Any, event: Any) -> None:
            self.bridge.start_turn(
                getattr(event, "task_prompt", None),
                metadata={
                    "event": getattr(event, "type", "agent_execution_started"),
                    "agent_role": getattr(getattr(event, "agent", None), "role", None),
                    "task": safe_text(getattr(event, "task", None), max_length=2000),
                },
            )

        @crewai_event_bus.on(AgentExecutionCompletedEvent)
        def on_agent_completed(source: Any, event: Any) -> None:
            self.bridge.assistant_text(
                getattr(event, "output", None),
                metadata={"event": getattr(event, "type", "agent_execution_completed")},
            )

        @crewai_event_bus.on(AgentExecutionErrorEvent)
        def on_agent_error(source: Any, event: Any) -> None:
            self.bridge.assistant_text(
                f"Agent error: {getattr(event, 'error', '')}",
                metadata={"event": getattr(event, "type", "agent_execution_error")},
            )

        @crewai_event_bus.on(ToolUsageStartedEvent)
        def on_tool_started(source: Any, event: Any) -> None:
            self.bridge.tool_start(
                tool_name=str(getattr(event, "tool_name", "tool")),
                tool_input=getattr(event, "tool_args", None),
                run_id=crewai_tool_run_id(event),
                metadata=event_metadata(event),
            )

        @crewai_event_bus.on(ToolUsageFinishedEvent)
        def on_tool_finished(source: Any, event: Any) -> None:
            self.bridge.tool_end(
                output=getattr(event, "output", None),
                run_id=crewai_tool_run_id(event),
                metadata=event_metadata(event),
            )

        @crewai_event_bus.on(ToolUsageErrorEvent)
        def on_tool_error(source: Any, event: Any) -> None:
            self.bridge.tool_end(
                output=getattr(event, "error", None),
                run_id=crewai_tool_run_id(event),
                status="error",
                metadata=event_metadata(event),
            )


def safe_run_id(value: Any) -> str:
    if value is None:
        return ""
    return str(value).replace("/", "-").replace("\\", "-")


def format_display_name(framework: str, agent_label: str) -> str:
    return f"{framework}:{agent_label}"


def extract_prompt(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, Mapping):
        for key in ("input", "prompt", "question", "query"):
            if key in value and value[key] is not None:
                return safe_text(value[key])
        messages = value.get("messages")
        if isinstance(messages, list) and messages:
            return extract_message_text(messages[-1])
    return safe_text(value)


def extract_message_text(message: Any) -> str:
    if isinstance(message, str):
        return message
    if isinstance(message, Mapping):
        content = message.get("content")
    else:
        content = getattr(message, "content", None)
    if isinstance(content, list):
        return join_text_blocks(content)
    if content is not None:
        return safe_text(content)
    return safe_text(message)


def extract_llm_text(response: Any) -> str:
    generations = getattr(response, "generations", None)
    if isinstance(generations, list):
        texts: list[str] = []
        for generation_list in generations:
            if not isinstance(generation_list, list):
                generation_list = [generation_list]
            for generation in generation_list:
                text = getattr(generation, "text", None)
                if text:
                    texts.append(str(text))
                    continue
                message = getattr(generation, "message", None)
                if message is not None:
                    texts.append(extract_message_text(message))
        return "\n".join(t for t in texts if t)
    return ""


def event_metadata(event: Any) -> dict[str, Any]:
    fields = (
        "type",
        "agent_key",
        "agent_role",
        "agent_id",
        "tool_name",
        "tool_args",
        "tool_class",
        "run_attempts",
        "task_name",
        "task_id",
        "from_cache",
    )
    return {field: to_jsonable(getattr(event, field)) for field in fields if hasattr(event, field)}


def crewai_tool_run_id(event: Any) -> str:
    parts = [
        getattr(event, "task_id", None),
        getattr(event, "agent_id", None),
        getattr(event, "tool_name", None),
        getattr(event, "run_attempts", None),
    ]
    value = "-".join(str(part) for part in parts if part not in (None, ""))
    return safe_run_id(value or getattr(event, "type", "crewai-tool"))

"""Utilities shared by the Entire LangGraph/CrewAI adapter."""

from __future__ import annotations

import base64
import json
import logging
import os
import re
import shutil
import subprocess
import threading
import uuid
from dataclasses import is_dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable, Mapping

ENTIRE_AGENT_NAME = "entire-adapter"
ENTIRE_AGENT_TYPE = "Entire Adapter"
PROTOCOL_VERSION = 1
DEFAULT_TIMEOUT_SECONDS = 5.0
MAX_TEXT_LENGTH = 20_000

logger = logging.getLogger("entire_adapter")
_warned_keys: set[str] = set()
_warned_lock = threading.Lock()


class EntireAdapterError(RuntimeError):
    """Raised only when strict mode is enabled."""


class EntireCommandResult:
    """Small subprocess result wrapper used by tests and callers."""

    def __init__(
        self,
        args: list[str],
        returncode: int,
        stdout: str = "",
        stderr: str = "",
        skipped: bool = False,
    ) -> None:
        self.args = args
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr
        self.skipped = skipped

    @property
    def ok(self) -> bool:
        return self.returncode == 0 and not self.skipped


def warn_once(key: str, message: str, *, log: logging.Logger | None = None) -> None:
    """Log a warning once per process."""

    with _warned_lock:
        if key in _warned_keys:
            return
        _warned_keys.add(key)
    (log or logger).warning(message)


def utc_now_iso() -> str:
    """Return an RFC 3339 timestamp with a Z suffix."""

    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def slugify(value: str, *, default: str = "agent", max_length: int = 48) -> str:
    """Return a path-safe slug that also satisfies Entire session ID validation."""

    value = value.strip().lower()
    value = re.sub(r"[^a-z0-9_-]+", "-", value)
    value = re.sub(r"-{2,}", "-", value).strip("-_")
    if not value:
        value = default
    return value[:max_length]


def new_session_id(framework: str, agent_label: str | None = None) -> str:
    """Create a distinct, Entire-safe session ID."""

    prefix = slugify("-".join(part for part in [framework, agent_label or "agent"] if part))
    return f"{prefix}-{uuid.uuid4().hex[:16]}"


def safe_text(value: Any, *, max_length: int = MAX_TEXT_LENGTH) -> str:
    """Convert arbitrary framework values into bounded text."""

    if value is None:
        return ""
    if isinstance(value, str):
        text = value
    else:
        try:
            text = json.dumps(to_jsonable(value), ensure_ascii=False, sort_keys=True)
        except Exception:
            text = repr(value)
    if len(text) > max_length:
        return text[:max_length] + "...[truncated]"
    return text


def to_jsonable(value: Any, *, max_depth: int = 5) -> Any:
    """Convert arbitrary Python objects into JSON-friendly values."""

    if max_depth < 0:
        return safe_text(value, max_length=500)
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, bytes):
        return base64.b64encode(value).decode("ascii")
    if isinstance(value, uuid.UUID):
        return str(value)
    if isinstance(value, Mapping):
        return {
            str(k): to_jsonable(v, max_depth=max_depth - 1)
            for k, v in value.items()
        }
    if isinstance(value, (list, tuple, set, frozenset)):
        return [to_jsonable(v, max_depth=max_depth - 1) for v in value]
    if is_dataclass(value):
        return to_jsonable(asdict(value), max_depth=max_depth - 1)
    model_dump = getattr(value, "model_dump", None)
    if callable(model_dump):
        try:
            return to_jsonable(model_dump(), max_depth=max_depth - 1)
        except Exception:
            pass
    dict_method = getattr(value, "dict", None)
    if callable(dict_method):
        try:
            return to_jsonable(dict_method(), max_depth=max_depth - 1)
        except Exception:
            pass
    content = getattr(value, "content", None)
    if content is not None:
        return to_jsonable(content, max_depth=max_depth - 1)
    return safe_text(value, max_length=2000)


def compact_metadata(metadata: Mapping[str, Any] | None) -> dict[str, str]:
    """Flatten metadata to the string map accepted by Entire's Event metadata."""

    if not metadata:
        return {}
    result: dict[str, str] = {}
    for key, value in metadata.items():
        if value is None:
            continue
        result[str(key)] = safe_text(value, max_length=2000)
    return result


def find_repo_root(path: str | os.PathLike[str] | None = None) -> Path:
    """Resolve the git worktree root, falling back to the current directory."""

    base = Path(path or os.getcwd()).resolve()
    try:
        completed = subprocess.run(
            ["git", "-C", str(base), "rev-parse", "--show-toplevel"],
            text=True,
            capture_output=True,
            check=False,
            timeout=2,
        )
        if completed.returncode == 0 and completed.stdout.strip():
            return Path(completed.stdout.strip()).resolve()
    except Exception:
        pass
    return base


def resolve_session_dir(repo_path: str | os.PathLike[str] | None = None) -> Path:
    """Return an adapter session directory outside the worktree when possible."""

    root = find_repo_root(repo_path)
    try:
        completed = subprocess.run(
            ["git", "-C", str(root), "rev-parse", "--git-common-dir"],
            text=True,
            capture_output=True,
            check=False,
            timeout=2,
        )
        if completed.returncode == 0 and completed.stdout.strip():
            git_common = Path(completed.stdout.strip())
            if not git_common.is_absolute():
                git_common = root / git_common
            return (git_common / "entire-adapter" / "sessions").resolve()
    except Exception:
        pass
    return (root / ".entire-adapter" / "sessions").resolve()


def resolve_session_file(session_dir: str | os.PathLike[str], session_id: str) -> Path:
    """Return the transcript path for a session ID."""

    safe_id = session_id.replace("/", "-").replace("\\", "-").strip() or "session"
    return Path(session_dir).resolve() / f"{safe_id}.jsonl"


class TranscriptWriter:
    """Append-only JSONL transcript writer."""

    def __init__(
        self,
        session_id: str,
        *,
        repo_path: str | os.PathLike[str] | None = None,
        path: str | os.PathLike[str] | None = None,
    ) -> None:
        self.session_id = session_id
        self.repo_path = find_repo_root(repo_path)
        self.path = Path(path).resolve() if path else resolve_session_file(
            resolve_session_dir(self.repo_path),
            session_id,
        )
        self._lock = threading.Lock()

    def append(self, record: Mapping[str, Any]) -> None:
        payload = dict(record)
        payload.setdefault("v", 1)
        payload.setdefault("agent", ENTIRE_AGENT_NAME)
        payload.setdefault("cli_version", os.environ.get("ENTIRE_CLI_VERSION", "unknown"))
        payload.setdefault("ts", utc_now_iso())
        data = json.dumps(to_jsonable(payload), ensure_ascii=False, separators=(",", ":"))
        with self._lock:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            with self.path.open("a", encoding="utf-8") as handle:
                handle.write(data)
                handle.write("\n")

    def read_bytes(self) -> bytes:
        try:
            return self.path.read_bytes()
        except FileNotFoundError:
            return b""


class EntireClient:
    """Thin wrapper around `entire hooks entire-adapter ...`."""

    def __init__(
        self,
        *,
        repo_path: str | os.PathLike[str] | None = None,
        strict: bool = False,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        entire_bin: str = "entire",
        log: logging.Logger | None = None,
    ) -> None:
        self.repo_path = find_repo_root(repo_path)
        self.strict = strict
        self.timeout = timeout
        self.entire_bin = entire_bin
        self.log = log or logger

    def emit_hook(self, hook_name: str, payload: Mapping[str, Any]) -> EntireCommandResult:
        args = [self.entire_bin, "hooks", ENTIRE_AGENT_NAME, hook_name]
        if shutil.which(self.entire_bin) is None:
            return self._handle_skip(
                args,
                "missing-entire",
                "Entire CLI is not installed or not on PATH; continuing without checkpoints.",
            )

        try:
            completed = subprocess.run(
                args,
                input=json.dumps(to_jsonable(payload), ensure_ascii=False),
                text=True,
                capture_output=True,
                cwd=str(self.repo_path),
                timeout=self.timeout,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            return self._handle_error(
                args,
                f"Entire hook {hook_name!r} timed out after {self.timeout:g}s.",
                stderr=str(exc),
            )
        except OSError as exc:
            return self._handle_error(
                args,
                f"Entire hook {hook_name!r} could not be launched: {exc}",
                stderr=str(exc),
            )

        result = EntireCommandResult(
            args=args,
            returncode=completed.returncode,
            stdout=completed.stdout,
            stderr=completed.stderr,
        )
        if completed.returncode != 0:
            stderr = completed.stderr.strip() or completed.stdout.strip()
            return self._handle_error(
                args,
                f"Entire hook {hook_name!r} failed with exit code {completed.returncode}: {stderr[:500]}",
                stderr=stderr,
                returncode=completed.returncode,
            )
        return result

    def _handle_skip(self, args: list[str], key: str, message: str) -> EntireCommandResult:
        if self.strict:
            raise EntireAdapterError(message)
        warn_once(key, message, log=self.log)
        return EntireCommandResult(args=args, returncode=0, skipped=True)

    def _handle_error(
        self,
        args: list[str],
        message: str,
        *,
        stderr: str = "",
        returncode: int = 1,
    ) -> EntireCommandResult:
        if self.strict:
            raise EntireAdapterError(message)
        warn_once(f"hook-error:{message}", message, log=self.log)
        return EntireCommandResult(args=args, returncode=returncode, stderr=stderr)


def enable_entire_adapter(
    *,
    repo_path: str | os.PathLike[str] | None = None,
    run: bool = False,
    telemetry: bool = False,
    timeout: float = 30.0,
) -> list[EntireCommandResult] | list[str]:
    """Print or run setup commands for this adapter.

    By default this returns the commands developers should run. Pass
    ``run=True`` to execute the Entire setup command best-effort.
    """

    telemetry_arg = "true" if telemetry else "false"
    commands = [
        "pip install -e .",
        f"entire enable --agent {ENTIRE_AGENT_NAME} --telemetry={telemetry_arg}",
        "entire agent list",
    ]
    if not run:
        for command in commands:
            print(command)
        return commands

    root = find_repo_root(repo_path)
    results: list[EntireCommandResult] = []
    if shutil.which("entire") is None:
        warn_once(
            "missing-entire-enable",
            "Entire CLI is not installed or not on PATH; cannot enable Entire Adapter automatically.",
        )
        return [
            EntireCommandResult(
                args=["entire", "enable", "--agent", ENTIRE_AGENT_NAME],
                returncode=0,
                skipped=True,
            )
        ]

    for args in (
        ["entire", "enable", "--agent", ENTIRE_AGENT_NAME, f"--telemetry={telemetry_arg}"],
        ["entire", "agent", "list"],
    ):
        completed = subprocess.run(
            args,
            text=True,
            capture_output=True,
            cwd=str(root),
            timeout=timeout,
            check=False,
        )
        results.append(
            EntireCommandResult(
                args=args,
                returncode=completed.returncode,
                stdout=completed.stdout,
                stderr=completed.stderr,
            )
        )
    return results


def join_text_blocks(blocks: Iterable[Any]) -> str:
    """Extract readable text from message/content block collections."""

    parts: list[str] = []
    for block in blocks:
        if isinstance(block, str):
            parts.append(block)
        elif isinstance(block, Mapping):
            text = block.get("text") or block.get("content")
            if text is not None:
                parts.append(safe_text(text))
        else:
            content = getattr(block, "content", None)
            if content is not None:
                parts.append(safe_text(content))
            else:
                parts.append(safe_text(block))
    return "\n".join(part for part in parts if part)

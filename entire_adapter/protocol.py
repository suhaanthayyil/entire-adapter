"""Entire external-agent protocol implementation for the Python adapter."""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
from pathlib import Path
from typing import Any, BinaryIO, TextIO

from .utils import (
    ENTIRE_AGENT_NAME,
    ENTIRE_AGENT_TYPE,
    PROTOCOL_VERSION,
    compact_metadata,
    resolve_session_dir,
    resolve_session_file,
    safe_text,
    to_jsonable,
    utc_now_iso,
)

HOOK_NAMES = ["session-start", "turn-start", "turn-end", "session-end"]
EVENT_TYPES = {
    "session-start": 1,
    "turn-start": 2,
    "turn-end": 3,
    "session-end": 5,
}


def main(argv: list[str] | None = None) -> None:
    code = handle(argv or sys.argv[1:], sys.stdin.buffer, sys.stdout.buffer, sys.stderr)
    raise SystemExit(code)


def handle(
    argv: list[str],
    stdin: BinaryIO,
    stdout: BinaryIO,
    stderr: TextIO,
) -> int:
    if not argv:
        stderr.write("usage: entire-agent-entire-adapter <subcommand> [args]\n")
        return 1

    command, args = argv[0], argv[1:]
    try:
        if command == "info":
            write_json(stdout, info_response())
        elif command == "detect":
            write_json(stdout, {"present": True})
        elif command == "get-session-id":
            payload = read_json(stdin)
            write_json(stdout, {"session_id": payload.get("session_id", "")})
        elif command == "get-session-dir":
            ns = parse_args(args, [("--repo-path", {"required": True})])
            write_json(stdout, {"session_dir": str(resolve_session_dir(ns.repo_path))})
        elif command == "resolve-session-file":
            ns = parse_args(
                args,
                [
                    ("--session-dir", {"required": True}),
                    ("--session-id", {"required": True}),
                ],
            )
            write_json(stdout, {"session_file": str(resolve_session_file(ns.session_dir, ns.session_id))})
        elif command == "read-session":
            payload = read_json(stdin)
            session_id = payload.get("session_id") or "entire-adapter-session"
            session_ref = payload.get("session_ref") or str(
                resolve_session_file(resolve_session_dir(payload.get("repo_path")), session_id)
            )
            write_json(
                stdout,
                {
                    "session_id": session_id,
                    "agent_name": ENTIRE_AGENT_NAME,
                    "repo_path": payload.get("repo_path") or os.environ.get("ENTIRE_REPO_ROOT", os.getcwd()),
                    "session_ref": session_ref,
                    "start_time": payload.get("timestamp") or utc_now_iso(),
                    "native_data": None,
                    "modified_files": [],
                    "new_files": [],
                    "deleted_files": [],
                },
            )
        elif command == "write-session":
            payload = read_json(stdin)
            write_session(payload)
        elif command == "read-transcript":
            ns = parse_args(args, [("--session-ref", {"required": True})])
            stdout.write(read_bytes(ns.session_ref))
        elif command == "chunk-transcript":
            ns = parse_args(args, [("--max-size", {"required": True, "type": int})])
            write_json(stdout, {"chunks": [b64(chunk) for chunk in chunk_bytes(stdin.read(), ns.max_size)]})
        elif command == "reassemble-transcript":
            payload = read_json(stdin)
            for chunk in payload.get("chunks", []):
                stdout.write(base64.b64decode(chunk))
        elif command == "format-resume-command":
            ns = parse_args(args, [("--session-id", {"required": True})])
            write_json(
                stdout,
                {
                    "command": (
                        "Rerun the LangGraph or CrewAI entrypoint that created "
                        f"Entire Adapter session {ns.session_id}"
                    )
                },
            )
        elif command == "parse-hook":
            ns = parse_args(args, [("--hook", {"required": True})])
            event = parse_hook(ns.hook, stdin.read())
            write_json(stdout, event)
        elif command == "install-hooks":
            write_json(stdout, {"hooks_installed": 0})
        elif command == "uninstall-hooks":
            return 0
        elif command == "are-hooks-installed":
            write_json(stdout, {"installed": True})
        elif command == "get-transcript-position":
            ns = parse_args(args, [("--path", {"required": True})])
            write_json(stdout, {"position": len(read_lines(ns.path))})
        elif command == "extract-modified-files":
            ns = parse_args(
                args,
                [
                    ("--path", {"required": True}),
                    ("--offset", {"required": True, "type": int}),
                ],
            )
            files = extract_modified_files(ns.path, ns.offset)
            write_json(stdout, {"files": files, "current_position": len(read_lines(ns.path))})
        elif command == "extract-prompts":
            ns = parse_args(
                args,
                [
                    ("--session-ref", {"required": True}),
                    ("--offset", {"required": True, "type": int}),
                ],
            )
            write_json(stdout, {"prompts": extract_prompts(ns.session_ref, ns.offset)})
        elif command == "extract-summary":
            ns = parse_args(args, [("--session-ref", {"required": True})])
            summary = extract_summary(ns.session_ref)
            write_json(stdout, {"summary": summary, "has_summary": bool(summary)})
        elif command == "compact-transcript":
            ns = parse_args(args, [("--session-ref", {"required": True})])
            compacted = compact_transcript(ns.session_ref)
            write_json(stdout, {"transcript": b64(compacted), "assets": []})
        else:
            stderr.write(f"unknown subcommand: {command}\n")
            return 1
    except Exception as exc:
        stderr.write(f"{exc}\n")
        return 1
    return 0


def info_response() -> dict[str, Any]:
    return {
        "protocol_version": PROTOCOL_VERSION,
        "name": ENTIRE_AGENT_NAME,
        "type": ENTIRE_AGENT_TYPE,
        "description": "Entire Adapter - LangGraph and CrewAI callback bridge",
        "is_preview": True,
        "protected_dirs": [".entire-adapter"],
        "protected_files": [],
        "hook_names": HOOK_NAMES,
        "capabilities": {
            "hooks": True,
            "transcript_analyzer": True,
            "transcript_preparer": False,
            "token_calculator": False,
            "compact_transcript": True,
            "text_generator": False,
            "hook_response_writer": False,
            "subagent_aware_extractor": False,
        },
    }


def parse_hook(hook_name: str, data: bytes) -> dict[str, Any] | None:
    if hook_name not in EVENT_TYPES:
        return None
    payload = json.loads(data.decode("utf-8")) if data.strip() else {}
    raw_data = payload.get("raw_data") or {}
    metadata = {
        "framework": raw_data.get("framework"),
        "agent_label": raw_data.get("agent_label"),
        "hook_type": payload.get("hook_type") or hook_name,
        "tool_name": payload.get("tool_name"),
        "tool_use_id": payload.get("tool_use_id"),
    }
    raw_metadata = raw_data.get("metadata")
    if isinstance(raw_metadata, dict):
        metadata.update(raw_metadata)

    event: dict[str, Any] = {
        "type": EVENT_TYPES[hook_name],
        "session_id": payload.get("session_id") or "entire-adapter-session",
        "session_ref": payload.get("session_ref") or "",
        "timestamp": payload.get("timestamp") or utc_now_iso(),
        "model": payload.get("model") or "",
        "metadata": compact_metadata(metadata),
    }
    if hook_name == "turn-start":
        event["prompt"] = payload.get("user_prompt") or payload.get("prompt") or ""
    return event


def parse_args(args: list[str], specs: list[tuple[str, dict[str, Any]]]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(add_help=False)
    for name, kwargs in specs:
        parser.add_argument(name, **kwargs)
    return parser.parse_args(args)


def read_json(stdin: BinaryIO) -> dict[str, Any]:
    data = stdin.read()
    if not data.strip():
        return {}
    return json.loads(data.decode("utf-8"))


def write_json(stdout: BinaryIO, payload: Any) -> None:
    stdout.write(json.dumps(to_jsonable(payload), separators=(",", ":")).encode("utf-8"))
    stdout.write(b"\n")


def write_session(payload: dict[str, Any]) -> None:
    session_ref = payload.get("session_ref")
    native_data = payload.get("native_data")
    if not session_ref or not native_data:
        return
    path = Path(session_ref)
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(native_data, str):
        path.write_bytes(base64.b64decode(native_data))
    else:
        path.write_text(safe_text(native_data), encoding="utf-8")


def read_bytes(path: str | os.PathLike[str]) -> bytes:
    try:
        return Path(path).read_bytes()
    except FileNotFoundError:
        return b""


def read_lines(path: str | os.PathLike[str]) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for line in read_bytes(path).decode("utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            value = {"type": "assistant", "content": line}
        if isinstance(value, dict):
            records.append(value)
    return records


def chunk_bytes(data: bytes, max_size: int) -> list[bytes]:
    if max_size <= 0:
        raise ValueError("--max-size must be positive")
    if not data:
        return [b""]
    chunks: list[bytes] = []
    current = bytearray()
    for line in data.splitlines(keepends=True):
        if len(line) > max_size:
            if current:
                chunks.append(bytes(current))
                current.clear()
            for i in range(0, len(line), max_size):
                chunks.append(line[i : i + max_size])
            continue
        if current and len(current) + len(line) > max_size:
            chunks.append(bytes(current))
            current.clear()
        current.extend(line)
    if current:
        chunks.append(bytes(current))
    return chunks


def b64(data: bytes) -> str:
    return base64.b64encode(data).decode("ascii")


def extract_prompts(path: str | os.PathLike[str], offset: int = 0) -> list[str]:
    prompts: list[str] = []
    for record in read_lines(path)[max(offset, 0) :]:
        if record.get("type") != "user":
            continue
        text = content_text(record.get("content"))
        if text:
            prompts.append(text)
    return prompts


def extract_summary(path: str | os.PathLike[str]) -> str:
    for record in reversed(read_lines(path)):
        if record.get("type") == "assistant":
            text = content_text(record.get("content"))
            if text:
                return text[:4000]
    prompts = extract_prompts(path)
    return prompts[-1][:4000] if prompts else ""


def extract_modified_files(path: str | os.PathLike[str], offset: int = 0) -> list[str]:
    files: list[str] = []
    seen: set[str] = set()
    for record in read_lines(path)[max(offset, 0) :]:
        for candidate in iter_path_candidates(record):
            normalized = candidate.strip()
            if is_probable_path(normalized) and normalized not in seen:
                seen.add(normalized)
                files.append(normalized)
    return files


def iter_path_candidates(value: Any) -> list[str]:
    found: list[str] = []
    if isinstance(value, dict):
        for key, item in value.items():
            lowered = str(key).lower()
            if lowered in {"path", "file", "file_path", "filepath", "filename", "filepaths", "files"}:
                if isinstance(item, str):
                    found.append(item)
                elif isinstance(item, list):
                    found.extend(str(v) for v in item if isinstance(v, str))
            found.extend(iter_path_candidates(item))
    elif isinstance(value, list):
        for item in value:
            found.extend(iter_path_candidates(item))
    return found


def is_probable_path(value: str) -> bool:
    if not value or "\n" in value or len(value) > 300:
        return False
    if value.startswith(("http://", "https://")):
        return False
    return "/" in value or "\\" in value or "." in Path(value).name


def compact_transcript(path: str | os.PathLike[str]) -> bytes:
    lines: list[str] = []
    for record in read_lines(path):
        normalized = {
            "v": int(record.get("v") or 1),
            "agent": record.get("agent") or ENTIRE_AGENT_NAME,
            "cli_version": record.get("cli_version") or os.environ.get("ENTIRE_CLI_VERSION", "unknown"),
            "type": record.get("type") if record.get("type") in {"user", "assistant"} else "assistant",
            "ts": record.get("ts") or record.get("timestamp") or utc_now_iso(),
            "content": normalize_content(record.get("content")),
        }
        for key in ("framework", "agent_label", "run_id", "tool_name", "metadata"):
            if key in record:
                normalized[key] = to_jsonable(record[key])
        lines.append(json.dumps(normalized, separators=(",", ":"), ensure_ascii=False))
    if not lines:
        return b""
    return ("\n".join(lines) + "\n").encode("utf-8")


def normalize_content(content: Any) -> Any:
    if content is None:
        return ""
    if isinstance(content, (str, list)):
        return to_jsonable(content)
    return safe_text(content)


def content_text(content: Any) -> str:
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if isinstance(item, str):
                parts.append(item)
            elif isinstance(item, dict):
                if "text" in item:
                    parts.append(str(item["text"]))
                elif item.get("type") == "tool_use":
                    name = item.get("name") or "tool"
                    result = item.get("result")
                    parts.append(f"Tool {name}: {safe_text(result)}")
            else:
                parts.append(safe_text(item))
        return "\n".join(part for part in parts if part)
    return safe_text(content)


if __name__ == "__main__":
    main()

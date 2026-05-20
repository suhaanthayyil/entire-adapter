from __future__ import annotations

import logging
import threading

import pytest

from entire_adapter.utils import AsyncHookDispatcher, EntireAdapterError, EntireClient, new_session_id, warn_once


def test_new_session_id_is_path_safe():
    session_id = new_session_id("LangGraph", "review agent")
    assert "/" not in session_id
    assert "\\" not in session_id
    assert session_id.startswith("langgraph-review-agent-")


def test_missing_entire_warns_once(caplog):
    caplog.set_level(logging.WARNING, logger="entire_adapter")
    client = EntireClient(entire_bin="definitely-not-entire", strict=False)

    first = client.emit_hook("turn-end", {"session_id": "s"})
    second = client.emit_hook("turn-end", {"session_id": "s"})

    assert first.skipped is True
    assert second.skipped is True
    messages = [record.message for record in caplog.records if "Entire CLI is not installed" in record.message]
    assert len(messages) <= 1


def test_missing_entire_strict_raises():
    client = EntireClient(entire_bin="definitely-not-entire", strict=True)
    with pytest.raises(EntireAdapterError):
        client.emit_hook("turn-end", {"session_id": "s"})


def test_entire_client_uses_configured_agent_name(monkeypatch, tmp_path):
    captured = {}

    class Completed:
        returncode = 0
        stdout = ""
        stderr = ""

    def fake_run(args, **kwargs):
        captured["args"] = args
        captured["input"] = kwargs["input"]
        captured["cwd"] = kwargs["cwd"]
        return Completed()

    monkeypatch.setattr("entire_adapter.utils.shutil.which", lambda _: "/usr/bin/entire")
    monkeypatch.setattr("entire_adapter.utils.subprocess.run", fake_run)

    client = EntireClient(repo_path=tmp_path, entire_agent_name="langgraph")
    result = client.emit_hook("turn-end", {"session_id": "s"})

    assert result.ok is True
    assert captured["args"] == ["entire", "hooks", "langgraph", "turn-end"]
    assert '"session_id": "s"' in captured["input"]


def test_warn_once_deduplicates(caplog):
    caplog.set_level(logging.WARNING, logger="entire_adapter")
    log = logging.getLogger("entire_adapter")
    warn_once("unit-test-key", "hello", log=log)
    warn_once("unit-test-key", "hello", log=log)
    assert [record.message for record in caplog.records].count("hello") == 1


def test_async_dispatcher_flushes_and_closes():
    client = EntireClient(entire_bin="definitely-not-entire", entire_agent_name="langgraph")
    calls = []
    client.emit_hook = lambda hook, payload: calls.append((hook, payload))  # type: ignore[method-assign]

    dispatcher = AsyncHookDispatcher(client, queue_size=4)
    dispatcher.emit_hook("turn-end", {"session_id": "s"})

    assert dispatcher.flush(2.0) is True
    dispatcher.close(2.0)
    assert calls == [("turn-end", {"session_id": "s"})]


def test_async_dispatcher_queue_full_warns_or_raises(caplog):
    caplog.set_level(logging.WARNING, logger="entire_adapter")
    started = threading.Event()
    release = threading.Event()

    def slow_emit(hook, payload):
        started.set()
        release.wait(2.0)

    client = EntireClient(entire_bin="definitely-not-entire", entire_agent_name="langgraph")
    client.emit_hook = slow_emit  # type: ignore[method-assign]
    dispatcher = AsyncHookDispatcher(client, queue_size=1, strict=False)
    dispatcher.emit_hook("turn-end", {"n": 1})
    assert started.wait(1.0)
    dispatcher.emit_hook("turn-end", {"n": 2})

    skipped = dispatcher.emit_hook("turn-end", {"n": 3})
    assert skipped.skipped is True
    assert "async hook queue is full" in caplog.text
    release.set()
    dispatcher.close(2.0)

    started = threading.Event()
    release = threading.Event()
    strict_client = EntireClient(entire_bin="definitely-not-entire", entire_agent_name="langgraph")
    strict_client.emit_hook = slow_emit  # type: ignore[method-assign]
    strict_dispatcher = AsyncHookDispatcher(strict_client, queue_size=1, strict=True)
    strict_dispatcher.emit_hook("turn-end", {"n": 1})
    assert started.wait(1.0)
    strict_dispatcher.emit_hook("turn-end", {"n": 2})
    with pytest.raises(EntireAdapterError):
        strict_dispatcher.emit_hook("turn-end", {"n": 3})
    release.set()
    strict_dispatcher.close(2.0)

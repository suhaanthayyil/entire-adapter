from __future__ import annotations

import logging

import pytest

from entire_adapter.utils import EntireAdapterError, EntireClient, new_session_id, warn_once


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


def test_warn_once_deduplicates(caplog):
    caplog.set_level(logging.WARNING, logger="entire_adapter")
    log = logging.getLogger("entire_adapter")
    warn_once("unit-test-key", "hello", log=log)
    warn_once("unit-test-key", "hello", log=log)
    assert [record.message for record in caplog.records].count("hello") == 1

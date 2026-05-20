from __future__ import annotations

import json
import os
import shutil
import subprocess
from pathlib import Path

import pytest


pytestmark = pytest.mark.skipif(
    os.environ.get("ENTIRE_E2E") != "1" or os.environ.get("CREWAI_E2E") != "1",
    reason="set ENTIRE_E2E=1 CREWAI_E2E=1 to run optional CrewAI smoke tests",
)


def test_crewai_external_agent_and_listener_smoke(tmp_path: Path):
    if shutil.which("entire-agent-crewai") is None:
        pytest.skip("entire-agent-crewai is not on PATH")
    try:
        import crewai  # noqa: F401
    except Exception as exc:
        pytest.skip(f"CrewAI is not importable: {exc}")

    from entire_adapter import EntireCrewAIListener

    listener = EntireCrewAIListener(
        agent_label="e2e-crew",
        repo_path=str(tmp_path),
        hook_dispatch="async",
    )
    assert listener.bridge.entire_agent_name == "crewai"
    listener.close(2.0)

    completed = subprocess.run(
        ["entire-agent-crewai", "info"],
        text=True,
        capture_output=True,
        check=False,
    )
    assert completed.returncode == 0, completed.stdout + completed.stderr
    assert json.loads(completed.stdout)["name"] == "crewai"

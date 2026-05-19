# Entire Adapter Live Test

Copy and paste these commands from the package root:

```bash
cd /Users/suhaan/Documents/Coding/entire

python3 -m venv adapter_live_test/.venv
source adapter_live_test/.venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e ".[langgraph,dev]"

SANDBOX_REPO="$(mktemp -d /Users/suhaan/Documents/Coding/entire/adapter_live_test/sandbox_repo.XXXXXX)"
echo "Sandbox repo: ${SANDBOX_REPO}"

cd "${SANDBOX_REPO}"
git init
git config user.name "Entire Adapter Test"
git config user.email "entire-adapter-test@example.com"
printf "# Entire Adapter Sandbox\n" > README.md
git add README.md
git commit -m "initial sandbox commit"

entire enable --agent entire-adapter --telemetry=false
entire agent list

python /Users/suhaan/Documents/Coding/entire/adapter_live_test/live_langgraph_agent.py "${SANDBOX_REPO}" | tee /Users/suhaan/Documents/Coding/entire/adapter_live_test/last_run.log

SESSION_ID="$(awk -F': ' '/Entire session:/ {print $2}' /Users/suhaan/Documents/Coding/entire/adapter_live_test/last_run.log | tail -1)"
echo "Session: ${SESSION_ID}"

git status --short
git add .
git commit -m "test entire adapter live checkpoint"

entire checkpoint list --session "${SESSION_ID}"
```

These commands will:

1. Create a local virtual environment in `adapter_live_test/.venv`.
2. Install this package with the LangGraph and test dependencies.
3. Create a temporary sandbox Git repo under `adapter_live_test/sandbox_repo.*`.
4. Enable Entire for the sandbox repo with `entire-adapter`.
5. Run a LangGraph workflow that calls a LangChain tool.
6. Write `agent_output.txt` through that tool.
7. Commit the file change.
8. Print and run the exact `entire checkpoint list --session "<actual-session-id>"` command.

The important line in the output looks like:

```text
Entire session: langgraph-live-test-...
```

Use that real session id, not `<session-prefix>` with angle brackets.

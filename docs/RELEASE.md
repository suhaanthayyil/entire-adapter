# Release Guide

## Local verification

```bash
cd /Users/suhaan/Documents/Coding/entire
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e ".[langgraph,crewai,dev]"
python -m pytest -q
python -m build
python -m twine check dist/*
```

## GitHub

The intended repository URL is:

```text
https://github.com/suhaanthayyil/entire-adapter
```

Create and push:

```bash
git init
git branch -M main
git config user.name "suhaanthayyil"
git config user.email "suhaanthayyil@users.noreply.github.com"
git add .
git commit -m "Release entire-adapter MVP"
gh repo create suhaanthayyil/entire-adapter --public --source . --remote origin --push
```

Do not add a `Co-authored-by` trailer.

## PyPI

The package name is:

```text
entire-adapter
```

Build and upload:

```bash
rm -rf dist build *.egg-info
python -m build
python -m twine check dist/*
python -m twine upload dist/*
```

Use a PyPI API token owned by the maintainer account.

Install verification:

```bash
python -m pip install entire-adapter[langgraph]
entire-agent-entire-adapter info
```

# Release Guide

## Local verification

```bash
cd /Users/suhaan/Documents/Coding/entire
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e ".[langgraph,crewai,dev]"
python -m pytest -q
# Optional when Entire CLI is installed:
# ENTIRE_E2E=1 python -m pytest tests/e2e -q
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
git commit -m "Release entire-adapter 0.2.0"
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

## PyPI Trusted Publishing

This repo also includes `.github/workflows/publish.yml`, which publishes on GitHub Release creation through PyPI Trusted Publishing.

Configure a pending publisher on PyPI with:

```text
PyPI project name: entire-adapter
Owner: suhaanthayyil
Repository name: entire-adapter
Workflow name: publish.yml
Environment name: pypi
```

Then create a GitHub release:

```bash
git tag v0.2.0
git push origin v0.2.0
gh release create v0.2.0 --title "entire-adapter 0.2.0" --notes-file CHANGELOG.md
```

The GitHub Action will build and publish the wheel/sdist to PyPI without storing a PyPI token in GitHub secrets.

Install verification:

```bash
python -m pip install "entire-adapter[langgraph]"
entire-agent-langgraph info
entire-agent-crewai info
entire-agent-entire-adapter info
```

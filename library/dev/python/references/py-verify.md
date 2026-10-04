---
id: py-verify
domain: dev
document: checking Python code: ruff, the type checker, pytest and uv
---

# The Python gate and where it bends

Read the project's gate before using this one. A Makefile, a `noxfile.py`, a `tox.ini`, a pre-commit config or a CI workflow that names a lint, type or test command wins: it is what the project's own review runs.

## The runner

Every command runs through the project's own environment, never a Python found on PATH.

| The project has | Prefix every command with |
| --- | --- |
| `uv.lock` | `uv run` |
| `poetry.lock` | `poetry run` |
| `[tool.hatch.envs]` | `hatch run` |
| a `.venv` and a requirements file | `.venv/bin/python -m`, or `.venv\Scripts\python -m` on Windows |

`uv run` syncs the environment to the lockfile first; `uv run --locked` fails instead when the lockfile is out of step with pyproject.toml. Never `pip install` into the system Python or `uv pip install` into a uv project. A dependency the task needs goes in with `uv add` or `poetry add`, which updates the lockfile. A tool the project does not have is reported as missing, not installed.

## Each edit

```sh
ruff check <files>
ruff format --check <files>
```

Then the project's type checker on the same files: `mypy`, `pyright`, or `ty check` where the project already uses ty. Run it the way the project's gate does, on the package when the gate names one. A project with no type checker gets none added.

`ruff check` reads `[tool.ruff]` from pyproject.toml, or `ruff.toml`, and exits 1 on any finding. Only the codes the config selects run, and ruff's default set changes between versions: `ruff check --show-settings <file>` prints what is on and the target version it read from `requires-python`. To look at one trap the config leaves off, `ruff check --select B904 <files>` reads the same files without touching the config; report what it finds, it is not the gate. `ruff rule <code>` explains a code. `ruff check --fix` and `ruff format` only on the files you changed, and read the diff they made; never `--unsafe-fixes` unasked.

## Before done

```sh
pytest <path>::<test>
pytest <path> -k '<expression>'
```

`::` selects one test, class or parametrised case; `-k` matches names. Exit status 5 means no test was collected, which is not a pass: say so. A covering test marked `skip` or `xfail` is reported, not counted.

- When `addopts` carries `--cov-fail-under`, a scoped run fails on coverage alone. Add `--no-cov` to the scoped run and leave coverage to the whole one.
- A misspelled marker is a new marker and selects nothing. When the config does not set `--strict-markers`, pass it on a run that uses `-m`.
- When the config sets no `filterwarnings`, run the changed tests once with `-W error`. A warning your change added then fails; one from a third-party package is reported, not fixed.
- An `async def` test runs only under the plugin the project uses, pytest-asyncio, anyio or trio, with its marker or its auto mode. Check it was counted as passed.

## Once at the end

- The whole test file or package the change sits in.
- After a dependency change: `uv lock --check`, or the project's equivalent, and `pip-audit` when the project lists it.

Each needs a tool the machine may not have. When one is missing, say the step was not run; never report it as passed.

## Reading a failure

Read the first failure, not the last: `pytest -x --tb=short` stops at it. A ruff message names its code; a mypy message ends with its code in brackets, which `# type: ignore[code]` would name. Fix one thing, check again, and stop after three attempts at the same one: report it with the output rather than reshaping the design to get past it.

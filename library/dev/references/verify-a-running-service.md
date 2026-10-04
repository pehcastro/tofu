---
id: verify-a-running-service
domain: dev
document: checking a service that runs: starting it, reaching it and reading its output
---

# Verify a running service by calling it

A clean typecheck says the types agree. A service is verified when it was started, every route was called, and its answers were checked against the data behind them. Never report a service as working without that proof.

## Start it

- bash with `background: true`, the server itself as the whole command: `npm start`, `node src/index.ts`. No `&`, `nohup` or `start /b`. Without `background` the call blocks until its deadline kills it.
- The result names the shell, such as `bash-1`. Logs, restart and stop go through that name.
- One server per port. If the shells screen already runs it, use that one.

## Wait for the port

- bash with `check_port` set to the port, read from the code or the start output, until it reports listening. A started process is not a ready one.
- Still not listening after a few checks: the shell tool with `op: logs`. A crash at start is the finding, reported with its first error line.

## Call every route

- `curl -sS -i 'http://localhost:<port>/<route>'` prints the status, the headers and the body. Read all three. Quote every URL: `?` and `&` are shell syntax.
- Every route the change touches, then the failures: a missing id (404), a malformed parameter (400), every value a sort or filter accepts. A 500 is always a defect.
- A header the code sets is in the response. A cached route shows its cache header, and a second identical call shows the hit.
- A route meant to be fast is timed over several runs: `curl -sS -o /dev/null -w '%{http_code} %{time_total}\n' '<url>'`.

## Compare the numbers with the data

- A count, total or sum is checked against a direct query on the same store, through the database's command line or a `node -e` with the project's own driver. Totals that disagree with their rows are a defect even when every status is 200.
- Paging: the total matches the query, the last page is short, and pages never overlap.

## Stop it

- The shell tool with `op: stop` and the shell's name ends its whole process tree. After a code change, `op: restart` runs the same command again.
- Never kill, taskkill or pkill a PID: a killed wrapper leaves its node child holding the port.
- Then `check_port` reports the port closed.

## Report

Per route: the command, the status, the header that mattered, and the body line that proves the claim. Each defect with the command that reproduces it. Each route not called, and why.

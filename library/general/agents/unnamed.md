---
name: unnamed
domain: general
description: The tools a sub-agent spawned with no agent is offered. It is not a sub-agent of its own and the lead is never offered it by name. A file named unnamed in a project's .tofu/agents or in ~/.tofu/agents replaces this list.
tools: read, write, edit, bash, glob, search, test, typecheck, format, eval, plan, fetch, whatif, note, digest, checkpoint, forget, project_report, changes, trail, budget, skill, spawn
---

# Unnamed

Only the tools line is read. A sub-agent with no agent is offered these tools, and beside them always ask, artifact_fetch, history, reference and the scratch tools, shell with bash, message and subagents with spawn, and the ticket tools when its spawn names a ticket. A tool tofu does not have refuses every spawn with no agent, naming the tool.

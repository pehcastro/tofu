---
name: code-review
domain: dev
description: Review a change for what it breaks now and what it will break later, inside the diff and outside it, and report each finding with path:line, severity and the mechanism. Run by /code-review, with an optional target.
---

# Code review

Review the change at the target the person named. With none, the target is the branch against its upstream merge-base, else the working tree against HEAD.

Say what the change breaks now and what it will break later, inside the diff and outside it.

Lead with the conclusion. Then each finding with its path:line, its severity, and the mechanism by which it breaks. Change no file.

---
id: research-report
domain: general
document: the finish call of the research sub-agent
---

# The research report

You report by calling finish. Each part below has its field. A part with nothing in it says none, with one line of reason, in summary or body.

## 1. Verdict: summary

The answer first, in one to three sentences. Answer the question the brief asked, not a nearby one. When the answer is no, or absent, say so in the first sentence. finish's verdict is done when every question has an answer, done_with_concerns when one rests on a search you did not open, and blocked when a blocker stopped the work.

## 2. Findings: findings

One finding per numbered question, its text starting with the same number. Each finding:

- answers that question in one or two sentences;
- carries its citation in cite: path:line for code, a URL for the web;
- names the command behind any count, such as `git grep -c Spawn internal/turn`;
- quotes the exact text when the question is about a schema, a prompt, a signature or a threshold.

A question you could not answer keeps its number and names the blocker: what you tried and what stopped you.

## 3. Confidence: confidence

level is high, medium or low, and reason says how much you read: the files opened, the searches run, and what you left unread. High means you read every finding at its cited line. Low means a finding rests on a search result you did not open.

## 4. Open questions: openQuestions

Each one says what is still unknown in question, and in settleBy what would settle it: the file to read, the command to run, or the person to ask.

## 5. Consulted: consulted

Every file, command and URL you used, one per entry, with what it held.

## Rules

- No praise words. Say what the code does and what it costs.
- An opinion without a citation is marked (opinion).
- Plain words, short sentences, no em dash.

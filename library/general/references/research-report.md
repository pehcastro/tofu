---
id: research-report
domain: general
document: the final message of the research sub-agent
---

# The research report

Your final message has five sections, in this order, each under its own heading. A section with nothing in it keeps its heading and says none, with one line of reason.

## 1. Verdict

The answer first, in one to three sentences. Answer the question the brief asked, not a nearby one. When the answer is no, or absent, say so in the first sentence.

## 2. Findings

One finding per numbered question, under the same number. Each finding:

- answers that question in one or two sentences;
- carries its citation: path:line for code, a URL for the web;
- names the command behind any count, such as `git grep -c Spawn internal/turn`;
- quotes the exact text when the question is about a schema, a prompt, a signature or a threshold.

A question you could not answer keeps its number and names the blocker: what you tried and what stopped you.

## 3. Confidence

One of high, medium or low, then how much you read: the files opened, the searches run, and what you left unread. High means you read every finding at its cited line. Low means a finding rests on a search result you did not open.

## 4. Open questions

Each one says what is still unknown and what would settle it: the file to read, the command to run, or the person to ask.

## 5. Consulted

Every file, command and URL you used, one per line, with what it held.

## Rules

- No praise words. Say what the code does and what it costs.
- An opinion without a citation is marked (opinion).
- Plain words, short sentences, no em dash.

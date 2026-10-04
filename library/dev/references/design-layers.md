---
id: design-layers
domain: dev
document: what a PRD, an HLD and an LLD each decide, and what each leaves to the others
related: prd-template, hld-template, lld-template
---

# Which document says what

Each layer only refines the one above it, and none restates or overrides a contract that already exists.

## The PRD: what and why

It carries the problem, the goals with a measurable success figure and its source, how things work today and what changes, the capabilities that already exist, scope in and out, the requirements with business rules, the data as business concepts, the non functional targets, and acceptance a person can check.

It does not carry an endpoint path, a table or a column type, an architecture diagram, or a final technology choice. It may suggest a direction; the HLD decides.

```text
right   Order: a user's purchase. Key attributes: number, amount, status, time placed.
wrong   id UUID PRIMARY KEY, created_at TIMESTAMP NOT NULL
```

## The HLD: how, costly decisions only

The test is the cost of changing your mind. A decision that is expensive to reverse, crosses a team, or carries real risk belongs here with its reason: component boundaries, the choice of store or queue, the data flow, the strategy for caching, retries, rollout and rollback. A decision that is cheap to change is left to the LLD or the code.

It carries a map from every PRD requirement to the section that answers it, the reuse inventory with a source per row, the decisions and their reasons, references to the contract rather than a second definition of it, data as concepts and indexing strategy, key flows as sequence diagrams or state machines, and the release and compatibility strategy.

It does not carry a function signature, a class, pseudocode, a TTL, a timeout, a retry count, a DDL script, a validation rule or an error message.

```text
right   product detail is cached per product; a write invalidates it, a TTL is the backstop
wrong   TTL = 3600s, retries = 3, exponential backoff from 100ms
```

## The LLD: how, at the level of code

It carries the module layout and what each package owns, the key interfaces and signatures, the data structures, pseudocode for the main path and the branches that matter, the error classes and how each is handled, concurrency, transactions and idempotency, configuration and flags with their defaults, the test design, and a map back to the PRD, the HLD and the contract.

It does not add a service, endpoint or permission the HLD did not define, does not redefine an interface the contract owns, and is not the full code.

## When a layer needs something the layer above did not decide

It goes back up. An LLD that needs a new queue sends the question to whoever owns the HLD; it does not add the queue and note that it was technically necessary. A PRD question found while writing the HLD goes to the person who owns the product. The layer that finds the gap writes the question, not the answer.

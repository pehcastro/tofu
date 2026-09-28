---
id: lld-template
domain: dev
document: testany-eng lld-writer, SKILL.md, references/lld-core-template.en.md with its 14 sections, and references/modules.md with its add-on modules
found: kept in a local archive of Chinese engineering plugins and not committed here
related: design-layers, hld-template
---

# Writing an LLD

An LLD turns the HLD's decisions into something a developer can build without guessing. Read the PRD, the HLD and the contract first. The LLD refines them; it never adds a boundary or rewrites an interface.

## The core, always present

```markdown
# LLD: <module or feature>

Refines: <PRD> <version>, <HLD> <version>
Contract: <path>
Status: draft

## Modules in this LLD
| module | included | reason when not | section |
One row per add-on below.

## Scope and assumptions
In scope. Out of scope, and where it is covered instead.
| assumption | based on | what breaks if it is wrong |
| dependency | kind | ready or not | owner |

## Layout
The directory tree, what each package owns, what it may import.

## Interfaces and signatures
The key interfaces in the project's language.
| contract operation | function that serves it |

## Data structures
Internal types, request and response types, enums with every value, and the conversions between them.

## Main flow and pseudocode
A sequence diagram for the main path. Pseudocode for the branches that matter. A state machine when there is state.

## Errors
| error class | code range | handling | retried or not |
Codes come from the contract; internal codes are added, never swapped.

## Concurrency, transactions, idempotency
| scenario | strategy | how |
| operations | same transaction or not | what rolls back |
| operation | idempotency key | strategy |

## Configuration and flags
| key | type | default | meaning | differs by environment |
| flag | default | rollout |

## Test design
What each layer tests, which dependencies are real and which are faked, the cases that matter with input and expected result.

## Map back
| upstream item | from | section here | covered |

## Open questions
| question | sections affected | owner |
```

## Add-on modules, included only when they apply

- Contract: an interface used outside the team. Contract reference, operation to function map, error and permission map, versioning.
- Storage and migration: anything persisted. Schema, indexes, migration and rollback, backfill, old data.
- Async and events: a queue, an event or a background job. Topics, message shape, ordering, idempotency, dead letters and retry.
- Infrastructure: resources change. Resource list, reused modules, permissions, environment variables.
- Observability: the change ships to production. Logs, metrics, traces, alerts.
- Security and compliance: permissions or personal data. Authentication, authorisation, data classification, masking, audit.
- Deployment and release: several environments, a staged rollout or a rollback. Steps, rollback, flags.
- Frontend: a screen. Routes, state, interaction flow, error states, performance limits.
- External integration: a third party. Endpoint, authentication, rate limits, retry, degraded path.
- Library or SDK: a public package. API surface, versioning, compatibility, how it is published.

## Checks before handing it over

- Every PRD requirement in scope reaches a section here.
- Every HLD decision is carried, and none is quietly changed.
- Every signature and error code matches the contract.
- Every add-on is either present or has a reason for being absent.
- The numbers the HLD left open, timeouts, retries, sizes, have values here.

---
id: hld-template
domain: dev
document: the sections of a high-level design and what goes in each
related: design-layers, prd-template, lld-template
---

# Writing an HLD

Read the PRD and the contract first, then the existing design notes, the project's config and its shared modules. The HLD decides what the PRD only suggested.

## The skeleton

```markdown
# HLD: <feature>

Refines: <PRD path> <version>
Contract: <path>
Status: draft

## Requirement map
| requirement | acceptance | section here | status |
Every in-scope requirement appears once. A requirement with no section is a gap, not an omission to hide.

## Today and what changes
| component | today | change |

## Architecture
A diagram of the components and what each owns. Where the boundaries are and who trusts whom.

## Reuse
| capability needed | candidates | decision and reason | found in |

## Decisions
| decision | choice | reason | rejected alternatives |
Only decisions that are costly to reverse. A stack choice that departs from what the project already uses says why.

## Interfaces
| interface | method | path | caller | defined in |
A reference to the contract. New interfaces go to the contract, not here.

## Data
Entities as concepts, indexing strategy, retention and archiving. No column types or lengths.

## Key flows
A sequence diagram or state machine per flow that matters.
| failure | strategy |

## Non functional strategy
Performance: caching, batching, async, as strategy.
Reliability: idempotency, retry, circuit breaking, as strategy with no counts.
Observability: which measures, which alerts.

## Compatibility and release
| PRD compatibility requirement | how it is met |
Rollout, feature flag, rollback and when to roll back.

## Risks and dependencies
| item | description | mitigation |
```

## Variants

- A feature with a screen adds the frontend architecture: routing, state ownership, and which calls a page depends on.
- An integration adds the adapter boundary, authentication with the third party, rate limits, and the degraded path.
- A refactor adds the old and new structure side by side and the steps that keep it working in between.
- An optimisation adds where the time or the risk goes today, measured, and which change moves which number.

## Checks before handing it over

- The requirement map covers every in-scope PRD requirement.
- Every suggestion in the PRD has a decision, and every decision has a reason.
- Every reuse row names where it was found.
- No function signature, pseudocode, TTL, timeout or retry count.
- Interfaces match the contract exactly; nothing is redefined.
- A developer could start the LLD from it without asking what was meant.

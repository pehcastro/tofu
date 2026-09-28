---
id: prd-template
domain: dev
document: testany-eng prd-writer, SKILL.md and its five templates, new-feature-backend, new-feature-ui, integration, refactoring and optimization, each in Chinese with an English sibling
found: kept in a local archive of Chinese engineering plugins and not committed here
related: design-layers, hld-template
---

# Writing a PRD

Read before you write: the existing PRDs and design notes, the project's config, and whatever the person pointed you at. Every statement about how the system works today names the file it came from. What you cannot find, you ask about or mark as unknown.

## The skeleton

```markdown
# PRD: <feature>

Status: draft
Version, date, author

## Problem
What hurts today, for whom, and how we know.

## Goals and success measures
| measure | now | target | source of the number | how it is measured |

## How it works today, and what changes
| item | before | after |
Who is affected: callers, existing flows, other systems.

## Capabilities that already exist
| capability | what it covers | fit | gap | suggestion | found in |
When nothing fits, the paths and names searched.

## Scope
In scope. Out of scope. Open questions.

## Requirements
REQ-1 <one statement>
  Rules: BR-1 <rule>, triggered when <condition>
  Input, output, side effects (a notification sent, a record written)
REQ-2 ...

## Data concepts
Entities, what they mean, their key attributes, how they relate. No fields or types.

## Non functional targets
Performance, reliability, security, compatibility with existing callers and data, release: rollout, rollback, feature flag. Targets only; the strategy is the HLD's.

## Risks
| risk | impact | likelihood | mitigation |

## Acceptance
AC-1 <name>
- [ ] <an observable condition a person can check>
```

## Variants

- A feature with a screen adds the pages, the states of each page (empty, loading, error, no permission) and the interaction rules.
- An integration adds the third party's limits, its failure modes, and what the product does while it is down.
- A refactor adds the behaviour that must not change and how that will be shown, since there is nothing new to accept.
- An optimisation adds the current value of every measure it wants to move, with its source. A target with no baseline cannot be accepted.

## Checks before handing it over

- Every requirement has at least one acceptance line a person could check.
- Every success measure has a source for its number or says how it will be measured.
- Every row in the capabilities table names where it was found.
- No endpoint path, table, column type, architecture diagram or final technology choice.
- A developer could write the HLD from it without asking what a word means.

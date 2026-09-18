# Changelog

Kept by hand, in the shape of keepachangelog.com, and versioned by semver.

Boji is personal and not released, so the public interface that a version promises is the command line and the file formats: the verbs and their flags, the exit codes, the catalog schema, the ledger row schema and the policy schema. A version says what changed for someone driving the binary or reading its files, not what changed inside it.

Until 1.0.0 the minor number carries breaking changes, which is what 0.x means.

## Unreleased

## 0.2.0 - 2026-09-18

The record. Every decision can be explained and re-scored without asking the model again.

### Added

- `boji why <id>` prints the chain behind a decision: every answer with its distribution, the verdict, the rule that fired, the threshold it compared against, and a line naming any question that sat inside the dead band
- `boji replay --point <name> --set <threshold>=<value>` re-scores every recorded decision against changed thresholds, with no network call in its import graph, and counts how many changed verdicts now disagree with a recorded outcome
- `boji check '<command>'` decides on a real shell command and records the row without blocking anything, and `boji label <id> <outcome>` attaches the answer a person would have given
- `boji lint comments` reports every comment in the tree with its position, using the standard library parser rather than a text match
- a policy: thresholds as catalog data, a closed verdict type, authority nouls that relax and never tighten, and a dead band taken from the measured rerun spread
- a state builder with a version derived from its own shape, recorded on the row

### Changed

- a ledger answer keeps the kind it was: a noul is a number, not a probability parsed out of a field named for a different answer kind. Schema 2, and rows written under schema 1 still read
- the request cap is two documented token limits instead of one invented byte number, with an estimator built from five measured points and deliberately pessimistic where it has no data
- a bare catalog name is refused when more than one version exists, naming every version it found, rather than silently taking the newest

### Fixed

- `boji check` put the command itself into the recent user messages, so a question about what the user asked for answered 0.91 about a request nobody made, relaxing a force-push from ask to allow. It is 0.03 and ask now
- `boji doctor` reported the key as missing while `boji judge` was using it, and now names where the key came from
- a converter existed three times and two copies silently dropped a choice option's criteria

## 0.1.0 - 2026-09-18

The instrument. A typed decision can be asked, validated, recorded and measured.

### Added

- `boji judge` reads a state and a question battery on standard input and prints typed answers, writing a row for every call and answering a repeat from cache with no network
- `boji bench api` measures latency by state size and question count, rerun agreement, the option ceiling and cost per decision, and writes a dated report
- `boji bench cost` compares Jev, two frontier models and a plain regular expression on the same six decisions
- `boji doctor`, `boji catalog resolve`, `boji version`
- the Jev wire with structural validation: probabilities that sum, a chosen option inside its criteria, an argmax that agrees, a score level count that matches
- the decision ledger, append only, one file per day, with the full distribution on every answer, and a replay cache keyed on canonical JSON
- the question catalog with a build-time linter, and a YAML subset parser written by hand rather than taking a dependency

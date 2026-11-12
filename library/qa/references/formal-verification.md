---
id: formal-verification
domain: qa
document: agreeing what verified means, then checking it formally and empirically
---

# Formal verification

## Agree on it first

Formal means the claim is written before the check, so the check can disagree with the code. Write it in plain words, with the person, before any model or test:

- states: the variables that matter and the values they take, what the system is, not how the code stores it
- steps: each atomic action that moves from one state to the next, and who takes it
- invariant: one sentence that is true in every reachable state, such as two holders never own one path
- eventually: one thing that must happen in the end, such as every started turn finishes or is stopped

Ask the person to confirm those four, in their words. That agreement is what verified means for the task; a check of anything else is not the one they asked for.

## Check it two ways

- bounded: a model checker such as TLC over the states and steps, or a property test that generates every ordering of two or three actors over a small input. Start at the smallest bound that can break the invariant, then grow it.
- empirical: drive the real thing and leave an artifact. To see a pending or racing state, hold one request open on purpose and act on the other; never sleep and hope.

Report them apart. The bounded check proves the design for the sizes it explored; the driven run proves the wiring for the one ordering it took. Say what neither covered.

## Map model to code

For each step, name the function that takes it, and for each state variable the field that holds it. A step with no function, or a field no step names, is where the model and the code disagree.

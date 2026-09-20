# The sift corpus

Ten assistant replies, copied on 2026-09-19 out of
`R:/work/nkz-harness/plugins/brevity/evals/runs/control/tNN.txt` for
BOJI-065.

They are the **control** arm of the brevity plugin's own benchmark: a session
run with no brevity rules loaded, so the replies are what an unconstrained model
writes. That is the material this ticket is about. The arms that ran with the
rules loaded were deliberately not taken, because a corpus of already short
replies cannot show whether sifting finds padding.

`tasks.tsv` carries the prompt that produced each reply, taken from
`evals/prompts.txt`, one per turn. It fills the `task` field of the Jev state,
so relevance has something to be relevant to.

`labels.tsv` is a hand label per paragraph, written by reading each reply whole
against its task. 57 paragraphs, 44 keep, 13 drop. The labelling rule is at the
top of that file. One person wrote them, and that person also wrote the code
being measured. It is weaker evidence than two labellers disagreeing and
resolving, and the report says so with the number.

Two edits were made to the bytes as copied. `control-t05.txt` and
`control-t10.txt` named an absolute directory on the recording machine, and that
string is replaced by `the project directory` in both. Nothing else was changed,
and no paragraph was reworded, split or joined.

The em dashes in these files are the source material, not this repository's
prose. They arrived by `cp` rather than by an editor, and they are the reason
the brevity arm scores what it scores.

`render_test.go` round trips every file through `Split`, `Render` and `Restore`
and fails if a byte moves, so a file cannot be edited here without the test
noticing. `corpus_test.go` fails if a paragraph arrives or leaves without a hand
label.

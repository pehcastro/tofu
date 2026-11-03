package search

type Degraded string

const (
	Unfiltered    Degraded = "unfiltered"
	IgnoreSkipped Degraded = "ignore_skipped"
	BinarySkipped Degraded = "binary_skipped"
	Fallback      Degraded = "fallback"
	Truncated     Degraded = "truncated"
	Partial       Degraded = "partial"
	Stale         Degraded = "stale"
	Stopped       Degraded = "stopped"
	NameMatched   Degraded = "name_matched"
)

func Note(state Degraded, detail string) string {
	var meaning string
	switch state {
	case Unfiltered:
		meaning = "no .gitignore was applied and ignored paths are included"
	case IgnoreSkipped:
		meaning = "an ignore file was not applied, so the walk listed paths git would hide"
	case BinarySkipped:
		meaning = "a file holding a null byte was not read"
	case Fallback:
		meaning = "a file did not parse, so lines came back instead of a whole declaration"
	case Truncated:
		meaning = "a cap or a budget cut the answer short"
	case Partial:
		meaning = "the walk could not read everything it listed"
	case Stale:
		meaning = "the file changed between the read and the write, so nothing was written"
	case Stopped:
		meaning = "the command was killed at its deadline and its output is gone"
	case NameMatched:
		meaning = "the answer was matched on the identifier rather than resolved through types, so two symbols sharing one name are one entry and a call through an interface or a function value is missed"
	default:
		panic("search: " + string(state) + " is not a degraded state")
	}
	return "degraded " + string(state) + ": " + meaning + ". " + detail +
		". this result is not a complete answer and must not be read as one"
}

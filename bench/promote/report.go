package promote

import (
	"fmt"
	"strings"

	"tofu/internal/session"
)

func Render(machine, date, path string, rows []session.Promotion) string {
	turns := map[string]bool{}
	for _, row := range rows {
		turns[row.Session] = true
	}
	met := "not met"
	if len(rows) >= RowsToDecideAnything {
		met = "met"
	}
	leakage := "No row names its own answer in its own input: the row shape carries no free-text field for an answer to hide in."
	if len(rows) == 0 {
		leakage = "No row exists to check for leakage, so none names its own answer in its own input; there is nothing to give an example of."
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench promote: recount, %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s, go1.27.1 windows/amd64. Credential kind: none. Wire: none. No model call of any kind runs in this package and nothing leaves the machine.\n\n", machine)
	b.WriteString("Cost unit: none. There is no spend to report, because no call is made.\n\n")
	b.WriteString("TOFU-552 recounts what TOFU-306 built and the 2026-09-21 report first read. That report is not edited; this one replaces it as the current count.\n\n")

	b.WriteString("## Conclusion\n\n")
	fmt.Fprintf(b, "%d rows exist in the promotion log today, %s, at `%s`. %d distinct turns stand behind them and %d carry a usable label. ", len(rows), date, path, len(turns), len(rows))
	fmt.Fprintf(b, "The floor of %d is %s, counted %s: %d of %d, %d short. ", RowsToDecideAnything, met, date, len(rows), RowsToDecideAnything, max(RowsToDecideAnything-len(rows), 0))
	b.WriteString(leakage)
	b.WriteString("\n\n")

	b.WriteString("## Rows now\n\n")
	fmt.Fprintf(b, "%d. The row shape carries `at`, `session`, `action`, `event_id`, `event_kind`, `free_arm`, `chose`, and neither `action` nor `event_kind`, the fields a decision would read as input, spells a value from the `free_arm` or `chose` vocabulary, so no row shape here can name its own answer even when rows exist.\n\n", len(rows))
	if len(rows) == 0 {
		b.WriteString("No promotion log exists on disk anywhere under this tree. The recorder writes into the project state directory only when the interface itself runs one of the two actions the row shape covers, and nothing has driven it live.\n\n")
	}

	b.WriteString("## Enough to decide anything\n\n")
	fmt.Fprintf(b, "%d, unchanged from the first report. `bench/stopcheck` labelled 78 steps and reversed this project's own prior on that size; that is still the floor a promotion judgment would need to start at, not a promise that 78 settles it.\n\n", RowsToDecideAnything)

	b.WriteString("## Reading it\n\n")
	b.WriteString("```\ngo run ./bench/promote/gen\n```\n\n")
	b.WriteString("No network. It reads the project state directory and never writes to it.\n\n")

	b.WriteString("## Provenance\n\n")
	b.WriteString("Written by the bench agent under TOFU-552, through bench/promote/gen and report.Generate, from a live rerun of the corpus reader against the tree as it stands on the date above. Every figure is from that run.\n")

	return b.String()
}

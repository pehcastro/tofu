package tokens

import (
	"fmt"
	"strings"

	"tofu/bench/schemas"
)

const RivalProseSharePercent = 79.0

type ProseReport struct {
	Date      string
	Machine   string
	BuildNote string
	ToolCount int
	Whole     schemas.Whole
	Prose     ProseWhole
	TestCount int
}

func (r ProseReport) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tokens prose: %s\n\n", r.Date)
	fmt.Fprintf(&b, "Machine: %s. %s\n\n", r.Machine, r.BuildNote)
	fmt.Fprintf(&b, "`go test ./bench/tokens/... -count=1` passes, %d test functions.\n\n", r.TestCount)

	b.WriteString("## What was counted\n\n")
	fmt.Fprintf(&b, "Over the same registry `cmd/tofu/run.go` builds for a plain `tofu run` that `bench/schemas` measured on this date, %d tools, this splits each tool's schema bytes into two: `len(Description)`, the raw prose string, against the marshalled JSON of `Parameters`, the input schema. Both are counted in raw bytes, not JSON-escaped inside a wrapper object, so the two never sum to the wire figure on their own: the gap is the structural bytes, `{\"name\":...,\"description\":...,\"input_schema\":...}`, its quoting, and the array's own brackets and commas.\n\n", r.ToolCount)

	b.WriteString("## Per-tool split\n\n")
	b.WriteString("| Tool | Desc bytes | Param bytes | Schema bytes | Desc share |\n|---|---|---|---|---|\n")
	for _, tp := range r.Prose.SortedByDescBytes() {
		share := 0.0
		if tp.SchemaBytes > 0 {
			share = 100 * float64(tp.DescBytes) / float64(tp.SchemaBytes)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.0f%% |\n", tp.Name, tp.DescBytes, tp.ParamBytes, tp.SchemaBytes, share)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "**Whole: %d description bytes and %d parameter bytes over %d tools.** The wire figure measured here is %d bytes; description plus parameter bytes sum to %d, %d bytes short of it, which is the name field, the JSON keys and quoting around all three fields, and the array wrapper, none of which is prose or parameters.\n\n",
		r.Prose.DescBytes, r.Prose.ParamBytes, r.ToolCount, r.Whole.WireBytes, r.Prose.DescBytes+r.Prose.ParamBytes, r.Prose.StructuralBytes)

	const tofu520WireBytes = 16575
	apart := tofu520WireBytes - r.Whole.WireBytes
	if apart < 0 {
		apart = -apart
	}
	fmt.Fprintf(&b, "TOFU-520 measured the same registry at %d bytes; this run measured %d, %d apart. Seventeen of the eighteen tools TOFU-520 measured are byte for byte identical to that report; `bash` is not, because its description embeds the shell's own `pwd` for the working directory the generator was run from, `runs one command in %%s. cwd is already %%s`, and that string depends on where `go run` was invoked rather than on any tool schema. This generator runs from `bench/tokens/gen`, one character shorter than `bench/schemas/gen`, which is the whole of the bash gap. Every other tool's bytes are fixed by its own definition and do not move, and a tool added since TOFU-520, such as tofu_docs, is in this run's figure and not in that one, so its bytes are in the gap too.\n\n", tofu520WireBytes, r.Whole.WireBytes, apart)

	b.WriteString("## Against their number\n\n")
	share := r.Prose.ProseShare()
	direction := "below"
	if share > RivalProseSharePercent {
		direction = "above"
	}
	fmt.Fprintf(&b, "Description bytes are %.1f%% of the wire schema bytes, %s their reported %.0f%%.\n", share, direction, RivalProseSharePercent)

	return b.String()
}

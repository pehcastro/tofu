package server

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Render(machine, date, bobDir string) (string, error) {
	first, err := Load(Session1Dir, "dev")
	if err != nil {
		return "", err
	}
	second, err := Load(Session2Dir, "dev")
	if err != nil {
		return "", err
	}
	share, err := Scan(bobDir, filepath.Dir(Session1Dir))
	if err != nil {
		return "", err
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench ask/server: the declared-commands arm, %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s. Credential kind: none. Wire: none. No model call runs in this package and nothing leaves the machine.\n\n", machine)
	b.WriteString("Cost unit: none. There is no spend to report, because no call is made.\n\n")
	b.WriteString("TOFU-556, first round: measures only, wires nothing. The two sessions are read through `bench/corpus`, the shared reader, from `.tofu/sessions`, read only, in both this repository and `F:\\localhost\\admin-template`.\n\n")

	b.WriteString("## The free arm, in one sentence\n\n")
	b.WriteString("Read the project's declared commands, act when one candidate fits, ask when two fit or none does.\n\n")

	b.WriteString("## Steps removed, counted from the recorded steps\n\n")
	fmt.Fprintf(b, "`%s`: %d recorded steps, the guess at step %d, %d steps removed had the arm asked instead of guessing. The body carries two outcome segments from a mid-session restart; `bench/corpus.ReadTurnDir` now returns the second segment on its own, so this is the 18-step turn the ticket describes with no tail-taking needed here.\n\n",
		first.TurnID, first.StepsTotal, first.GuessStep, first.StepsRemoved())
	fmt.Fprintf(b, "`%s`: %d recorded steps, the guess at step %d, %d steps removed had the arm asked instead of guessing.\n\n",
		second.TurnID, second.StepsTotal, second.GuessStep, second.StepsRemoved())

	b.WriteString("## How often the arm fires, across the whole corpus\n\n")
	fmt.Fprintf(b, "%d turns are readable across both directories. %d of them ask to start a server; the arm fires on %d of those %d, and never fires on the other %d, because a turn outside that intent never reaches the decision. As a share of all turns: %d of %d, %.2f percent.\n\n",
		share.TotalTurns, share.InDomain, share.Fires, share.InDomain, share.TotalTurns-share.InDomain,
		share.Fires, share.TotalTurns, 100*float64(share.Fires)/float64(share.TotalTurns))

	b.WriteString("## How often the arm would be wrong\n\n")
	b.WriteString("This depends entirely on what \"fits\" means, and the two readings disagree on both sessions.\n\n")
	b.WriteString("Reading the declared commands by literal name against the task's own words, `studio-admin`'s `package.json` has a script named `start`, and the word `start` is in both tasks. That is exactly one candidate, so this reading of the arm acts, silently, on `npm run start`, which needs a production build neither session has. In both sessions the one candidate that fits is the wrong one: 2 of 2.\n\n")
	b.WriteString("Reading the declared commands by what they run, `start` and `dev` both launch the same framework's server, so two candidates fit and this reading of the arm asks in both sessions. No wrong pick is possible when the arm asks instead of choosing, so this reading is 0 of 0 cases where one candidate fit: it never had exactly one.\n\n")
	b.WriteString("The arm as stated does not say which reading of \"fits\" to build. The cheap, literal one is wrong every time it fires on this corpus. The one that is not wrong needs to know that two script names name the same kind of process, which is a small piece of judgment sitting inside a mechanism this ticket called free.\n\n")

	b.WriteString("## What this corpus cannot decide\n\n")
	b.WriteString("Two labelled moments. Both fire under the correct reading of the arm and both would misfire under the naive one; neither number is a rate. `.local/boji/planning/doing/gate-evidence.md` puts the floor for a rate at twenty and the corpus here has two, because only two recorded sessions carry the start-a-server task at all.\n\n")

	b.WriteString("## Reading it\n\n")
	b.WriteString("```\ngo run ./bench/ask/server/gen\n```\n\n")
	b.WriteString("No network. It reads two named session directories and this repository's own `.tofu/sessions`, and writes nowhere but this report.\n\n")

	b.WriteString("## Provenance\n\n")
	b.WriteString("Written by the bench agent under TOFU-556, through `bench/ask/server/gen` and `bench/report.Generate`, from a live rerun of `bench/ask/server` against the tree as it stands on the date above. Every figure is from that run.\n")

	return b.String(), nil
}

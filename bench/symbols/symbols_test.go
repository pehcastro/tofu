package symbols

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/search"
	"tofu/internal/sys"
)

const runsPerQuery = 5

type corpus struct {
	language string
	root     string
	globs    []string
	call     string
	declared string
	names    []string
}

type tally struct {
	baseline, tool, agreed int
	spent                  []time.Duration
}

func TestSymbolsAgainstTheRipgrepPatternOverRustAndTypeScript(t *testing.T) {
	corpora := []corpus{
		{
			language: "rust", root: filepath.Join(sys.SourceRoot(), sys.DeskRoot), globs: []string{"*.rs"},
			call: `\b%s\s*(::<[^>]*>\s*)?\(`, declared: `\bfn\s+%s\b`,
			names: []string{
				"advance_next_nonspace", "allocations", "ask_questions", "board_card", "build_question", "channel", "close_here", "container", "desk_folder", "elbowed",
				"existing", "file_tree", "header_tabs", "indent_of", "kind_color", "lettered", "link_card", "mark_resolved", "menu_row", "offset",
			},
		},
		{
			language: "typescript", root: filepath.Join(sys.SourceRoot(), ".local", "sources", "pi-lens"), globs: []string{"*.ts", "*.tsx"},
			call: `\b%s\s*(<[^>]*>\s*)?\(`, declared: `\bfunction\s*\*?\s*%s\b`,
			names: []string{
				"applyFindingPolicy", "attributionKey", "boundedKeyLabel", "buildWordIndexAsync", "carriesComposedSql",
				"classifyLoopBlock", "clientRequestWorkspaceDiagnostics", "contentOfBytes", "createGenerationMap", "currentLinesMatchReadSnapshot",
			},
		},
	}
	for _, each := range corpora {
		files := tracked(t, each.root, each.globs)
		if len(files) == 0 {
			t.Logf("%s: SKIP, no file under %s, which is a clone kept outside git", each.language, each.root)
			continue
		}
		bodies := map[string][]string{}
		for _, rel := range files {
			raw, err := os.ReadFile(filepath.Join(each.root, rel))
			if err != nil {
				t.Fatal(err)
			}
			bodies[rel] = strings.Split(string(raw), "\n")
		}
		var total tally
		t.Logf("%s over %d files under %s", each.language, len(files), each.root)
		t.Logf("%-34s %5s %5s %6s %9s %10s", "name", "rg", "tool", "recall", "precision", "median")
		for _, name := range each.names {
			call, declared := regexp.MustCompile(fmt.Sprintf(each.call, name)), regexp.MustCompile(fmt.Sprintf(each.declared, name))
			baseline := map[string]string{}
			for _, rel := range files {
				for number, line := range bodies[rel] {
					if call.MatchString(line) && !declared.MatchString(line) {
						baseline[fmt.Sprintf("%s:%d", rel, number+1)] = strings.TrimSpace(line)
					}
				}
			}
			var graph search.Graph
			var spent []time.Duration
			for range runsPerQuery {
				started := time.Now()
				found, err := search.Symbols(each.root, files, name)
				spent = append(spent, time.Since(started))
				if err != nil {
					t.Fatal(err)
				}
				graph = found
			}
			found := map[string]bool{}
			for _, site := range graph.Callers {
				found[fmt.Sprintf("%s:%d", site.Path, site.Line)] = true
			}
			agreed := 0
			for site := range found {
				if _, both := baseline[site]; both {
					agreed++
				} else {
					t.Logf("    tool only %s", site)
				}
			}
			for site, line := range baseline {
				if !found[site] {
					t.Logf("    rg only   %s: %s", site, line)
				}
			}
			slices.Sort(spent)
			total.baseline, total.tool, total.agreed = total.baseline+len(baseline), total.tool+len(found), total.agreed+agreed
			total.spent = append(total.spent, spent[len(spent)/2])
			t.Logf("%-34s %5d %5d %6.2f %9.2f %10s", name, len(baseline), len(found), ratio(agreed, len(baseline)), ratio(agreed, len(found)), spent[len(spent)/2].Round(time.Microsecond))
		}
		slices.Sort(total.spent)
		t.Logf("%s total: rg %d, tool %d, agreed %d, recall %.3f, precision %.3f, median per query %s, slowest %s",
			each.language, total.baseline, total.tool, total.agreed, ratio(total.agreed, total.baseline), ratio(total.agreed, total.tool),
			total.spent[len(total.spent)/2].Round(time.Microsecond), total.spent[len(total.spent)-1].Round(time.Microsecond))
	}
}

func ratio(part, whole int) float64 {
	if whole == 0 {
		return 1
	}
	return float64(part) / float64(whole)
}

func tracked(t *testing.T, root string, globs []string) []string {
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	listed, err := exec.Command("git", append([]string{"-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "--"}, globs...)...).Output()
	if err != nil {
		t.Fatalf("listing %s: %v", root, err)
	}
	var files []string
	for rel := range strings.Lines(string(listed)) {
		rel = strings.TrimSpace(rel)
		if !slices.ContainsFunc(strings.Split(rel, "/"), func(segment string) bool { return segment == "vendor" || segment == "node_modules" }) {
			files = append(files, rel)
		}
	}
	return files
}

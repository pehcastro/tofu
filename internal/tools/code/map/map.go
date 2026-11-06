package repomap

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

const Name = "map"

type Limits struct {
	Budget         int
	BytesPerToken  int
	Damping        float64
	Iterations     int
	MentionWeight  float64
	SignatureBytes int
}

type Map struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Map{root: root, limits: limits}, err
}

func (Map) Name() string { return Name }

func (m Map) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "answers what matters in this repo for a task, in a token budget, before reading files one by one: the files ranked by a personalized pagerank over the go, rust and typescript reference graph, each with its top declarations as one-line signatures and their line numbers, cut to fit the budget. " +
			"task is the work in plain words; a word of it that is a declared identifier or a file or folder name seeds the rank, as fix Discounted returning 650, and files seeds it with paths. " +
			"budget is in tokens, " + strconv.Itoa(m.limits.Budget) + " when absent, and the answer says how many it used and how many files and declarations did not fit. " +
			"a reference is matched on the identifier, within one language and, for go, within the package or a package the file imports, so a name declared in several such files splits its weight between them. " +
			glob.IgnoredWalkDescription,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"task":   map[string]any{"type": "string"},
			"files":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"budget": map[string]any{"type": "integer"},
			"path":   map[string]any{"type": "string"},
		}},
	}
}

type request struct {
	Task   string   `json:"task"`
	Files  []string `json:"files"`
	Budget int      `json:"budget"`
	Path   string   `json:"path"`
}

type edge struct {
	from, to int
	name     string
	weight   float64
}

type decl struct {
	file      int
	at        engine.Definition
	score     float64
	mentioned bool
	signature string
}

type graph struct {
	files    []string
	at       map[string]int
	definers map[string][]int
	imports  map[int][]string
	words    map[string]bool
	rank     []float64
	decls    []decl
	seeded   string
}

func (m Map) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return contract.Result{}, fmt.Errorf("map: arguments are not the expected shape: %w", err)
	}
	if req.Budget < 0 {
		return contract.Failure("map: budget is a token count above zero, or absent for " + strconv.Itoa(m.limits.Budget)), nil
	}
	listed, err := glob.FilesUnder(m.root, cmp.Or(req.Path, "."), false)
	if err != nil {
		return contract.Result{}, fmt.Errorf("map: %w", err)
	}
	command := "map under " + listed.Under
	if len(listed.Files) == 0 {
		return contract.Result{Content: glob.WithNote("map: no files under "+listed.Under, listed.Note), Command: command}, nil
	}
	index := engine.Index(string(m.root), listed.Files, func(string) bool { return true })
	g := newGraph(index, req.Task)
	if len(g.definers) == 0 {
		return contract.Result{Content: glob.WithNote(fmt.Sprintf("map: no go, rust or typescript declarations in the %d files under %s", len(listed.Files), listed.Under), listed.Note), Command: command}, nil
	}
	teleport, refusal := m.seed(&g, req.Files)
	if refusal != "" {
		return contract.Failure("map: " + refusal), nil
	}
	edges, out := m.edges(g, index.Callers)
	g.rank = m.pagerank(teleport, edges, out)
	g.rankDecls(index.Definitions, edges, out)
	var notes []string
	if listed.Note != "" {
		notes = append(notes, strings.TrimSuffix(listed.Note, "\n"))
	}
	if index.Unparsed > 0 {
		notes = append(notes, fmt.Sprintf("%d files did not parse and are not ranked", index.Unparsed))
	}
	if index.Unreadable > 0 {
		notes = append(notes, fmt.Sprintf("%d files could not be read, the first %s", index.Unreadable, index.FirstFailed))
	}
	content, err := m.fit(g, listed.Under, cmp.Or(req.Budget, m.limits.Budget), notes)
	if err != nil {
		return contract.Failure("map: " + err.Error()), nil
	}
	return contract.Result{Content: content, Command: command}, nil
}

func newGraph(index engine.Graph, task string) graph {
	g := graph{at: map[string]int{}, definers: map[string][]int{}, imports: map[int][]string{}, words: map[string]bool{}}
	var paths []string
	for _, d := range index.Definitions {
		paths = append(paths, d.Path)
	}
	for _, site := range index.Callers {
		paths = append(paths, site.Path)
	}
	for _, rel := range slices.Compact(slices.Sorted(slices.Values(paths))) {
		g.at[rel] = len(g.files)
		g.files = append(g.files, rel)
	}
	for _, d := range index.Definitions {
		file := g.at[d.Path]
		if d.Kind == engine.KindImport {
			g.imports[file] = append(g.imports[file], d.Symbol)
		} else if name := bare(d.Symbol); !slices.Contains(g.definers[name], file) {
			g.definers[name] = append(g.definers[name], file)
		}
	}
	for _, word := range strings.FieldsFunc(task, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		g.words[word] = true
	}
	return g
}

func (m Map) seed(g *graph, named []string) ([]float64, string) {
	teleport := make([]float64, len(g.files))
	var listed, matched []string
	for _, one := range named {
		resolved, err := m.root.Resolve(one)
		if err != nil {
			return nil, err.Error()
		}
		rel, _ := filepath.Rel(string(m.root), resolved)
		file, known := g.at[filepath.ToSlash(rel)]
		if !known {
			return nil, one + " is not a go, rust or typescript file that declares or calls anything under this path"
		}
		teleport[file] = 1
		listed = append(listed, g.files[file])
	}
	for file, rel := range g.files {
		for _, segment := range strings.Split(strings.TrimSuffix(rel, path.Ext(rel)), "/") {
			if g.words[segment] {
				teleport[file] = 1
				matched = append(matched, segment)
			}
		}
	}
	for name := range g.words {
		for _, file := range g.definers[name] {
			teleport[file] = 1
			matched = append(matched, name)
		}
	}
	var through []string
	if len(listed) > 0 {
		through = append(through, "the files "+strings.Join(slices.Compact(slices.Sorted(slices.Values(listed))), ", "))
	}
	if len(matched) > 0 {
		through = append(through, "the task's words "+strings.Join(slices.Compact(slices.Sorted(slices.Values(matched))), ", "))
	}
	seeds := 0.0
	for _, w := range teleport {
		seeds += w
	}
	g.seeded = counted(int(seeds), "file") + ", through " + strings.Join(through, " and ")
	if seeds == 0 {
		g.seeded = "nothing: the task names no file, folder or declaration here, so this is the plain reference rank"
		for file := range teleport {
			teleport[file] = 1
		}
		seeds = float64(len(teleport))
	}
	for file := range teleport {
		teleport[file] /= seeds
	}
	return teleport, ""
}

func (m Map) edges(g graph, callers []engine.Site) ([]edge, []float64) {
	out := make([]float64, len(g.files))
	references := map[string]map[int]int{}
	for _, site := range callers {
		if references[site.Name] == nil {
			references[site.Name] = map[int]int{}
		}
		references[site.Name][g.at[site.Path]]++
	}
	var edges []edge
	for _, name := range slices.Sorted(maps.Keys(references)) {
		for _, from := range slices.Sorted(maps.Keys(references[name])) {
			reached := slices.DeleteFunc(slices.Clone(g.definers[name]), func(to int) bool { return !g.reaches(from, to) })
			if len(reached) == 0 {
				continue
			}
			weight := math.Sqrt(float64(references[name][from])) / float64(len(reached))
			if g.words[name] {
				weight *= m.limits.MentionWeight
			}
			for _, to := range reached {
				edges = append(edges, edge{from: from, to: to, name: name, weight: weight})
				out[from] += weight
			}
		}
	}
	return edges, out
}

func (g graph) reaches(from, to int) bool {
	caller, declarer := g.files[from], g.files[to]
	if language(caller) != language(declarer) {
		return false
	}
	if path.Ext(caller) != ".go" || path.Dir(caller) == path.Dir(declarer) {
		return true
	}
	return slices.ContainsFunc(g.imports[from], func(imported string) bool { return strings.HasSuffix(imported, "/"+path.Dir(declarer)) })
}

func language(rel string) string {
	if ext := path.Ext(rel); ext != ".tsx" {
		return ext
	}
	return ".ts"
}

func (m Map) pagerank(teleport []float64, edges []edge, out []float64) []float64 {
	rank := slices.Clone(teleport)
	for range m.limits.Iterations {
		next, dangling := make([]float64, len(rank)), 0.0
		for file, r := range rank {
			if out[file] == 0 {
				dangling += r
			}
		}
		for _, e := range edges {
			next[e.to] += m.limits.Damping * rank[e.from] * e.weight / out[e.from]
		}
		for file := range next {
			next[file] += (1 - m.limits.Damping + m.limits.Damping*dangling) * teleport[file]
		}
		rank = next
	}
	return rank
}

func (g *graph) rankDecls(definitions []engine.Definition, edges []edge, out []float64) {
	flow := map[string]float64{}
	for _, e := range edges {
		flow[strconv.Itoa(e.to)+" "+e.name] += g.rank[e.from] * e.weight / out[e.from]
	}
	for _, d := range definitions {
		if file, name := g.at[d.Path], bare(d.Symbol); d.Kind != engine.KindImport {
			g.decls = append(g.decls, decl{file: file, at: d, score: flow[strconv.Itoa(file)+" "+name], mentioned: g.words[name]})
		}
	}
	slices.SortStableFunc(g.decls, func(a, b decl) int {
		switch {
		case a.mentioned && !b.mentioned:
			return -1
		case b.mentioned && !a.mentioned:
			return 1
		}
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(g.rank[b.file], g.rank[a.file]), strings.Compare(a.at.Path, b.at.Path), cmp.Compare(a.at.FirstLine, b.at.FirstLine))
	})
}

func bare(symbol string) string {
	return symbol[strings.LastIndex(symbol, ".")+1:]
}

func (m Map) fit(g graph, under string, budget int, notes []string) (string, error) {
	declaring := map[int]bool{}
	for _, d := range g.decls {
		declaring[d.file] = true
	}
	head := func(files, decls, used int) string {
		return strings.Join(append([]string{
			fmt.Sprintf("map under %s: %d of %d files, %d of %d declarations, %d of %d tokens", under, files, len(declaring), decls, len(g.decls), used, budget),
			"seeded by " + g.seeded,
		}, notes...), "\n") + "\n"
	}
	foot := func(files, decls int) string {
		if files+decls == 0 {
			return "\nmore: none\n"
		}
		return fmt.Sprintf("\nmore: %d of %d files and %d of %d declarations did not fit the budget\n", files, len(declaring), decls, len(g.decls))
	}
	frame := head(len(declaring), len(g.decls), budget) + foot(len(declaring), len(g.decls))
	room := budget*m.limits.BytesPerToken - len(frame)
	if room < 0 {
		return "", fmt.Errorf("a budget of %d tokens is below the %d the header alone needs", budget, tokens(frame, m.limits.BytesPerToken))
	}
	bodies := map[string][]string{}
	chosen := map[int][]decl{}
	for _, d := range g.decls {
		line, err := m.signature(bodies, d.at)
		if err != nil {
			line = string(d.at.Kind) + " " + d.at.Symbol
		}
		d.signature = line
		cost := len(fmt.Sprintf("  %d %s\n", d.at.FirstLine, line))
		if len(chosen[d.file]) == 0 {
			cost += len(fmt.Sprintf("\n%s rank %.3f\n", g.files[d.file], g.rank[d.file]))
		}
		if cost > room {
			break
		}
		room -= cost
		chosen[d.file] = append(chosen[d.file], d)
	}
	var body strings.Builder
	shown := 0
	for _, file := range slices.SortedFunc(maps.Keys(chosen), func(a, b int) int {
		return cmp.Or(cmp.Compare(g.rank[b], g.rank[a]), strings.Compare(g.files[a], g.files[b]))
	}) {
		fmt.Fprintf(&body, "\n%s rank %.3f\n", g.files[file], g.rank[file])
		slices.SortFunc(chosen[file], func(a, b decl) int { return cmp.Compare(a.at.FirstLine, b.at.FirstLine) })
		for _, d := range chosen[file] {
			fmt.Fprintf(&body, "  %d %s\n", d.at.FirstLine, d.signature)
			shown++
		}
	}
	tail := body.String() + foot(len(declaring)-len(chosen), len(g.decls)-shown)
	used := 0
	for {
		measured := tokens(head(len(chosen), shown, used)+tail, m.limits.BytesPerToken)
		if measured == used {
			return head(len(chosen), shown, used) + tail, nil
		}
		used = measured
	}
}

func (m Map) signature(bodies map[string][]string, d engine.Definition) (string, error) {
	lines, read := bodies[d.Path]
	if !read {
		body, err := os.ReadFile(filepath.Join(string(m.root), filepath.FromSlash(d.Path)))
		if err != nil {
			return "", err
		}
		lines = strings.Split(string(body), "\n")
		bodies[d.Path] = lines
	}
	if d.FirstLine < 1 || d.FirstLine > len(lines) {
		return "", fmt.Errorf("line %d is outside %s", d.FirstLine, d.Path)
	}
	line := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(lines[d.FirstLine-1]), "{"))
	if len(line) > m.limits.SignatureBytes {
		line = strings.ToValidUTF8(line[:m.limits.SignatureBytes], "") + "..."
	}
	return line, nil
}

func counted(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func tokens(text string, bytesPerToken int) int {
	return (len(text) + bytesPerToken - 1) / bytesPerToken
}

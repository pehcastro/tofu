package cost_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/store/session/cost"
)

const (
	sessionID = "c0ffee00-0000-4000-8000-000000000001"
	idleGap   = 400
	seeds     = 24
)

type record struct {
	at   int
	line map[string]any
}

type stream struct {
	meta    map[string]any
	clock   int
	records []record
}

type script struct {
	rng     *rand.Rand
	seq     int
	streams map[string]*stream
	agents  []string
}

func newScript(seed uint64) *script {
	return &script{rng: rand.New(rand.NewPCG(seed, seed*7+1)), streams: map[string]*stream{"": {}}}
}

func (s *script) id(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s%04d", prefix, s.seq)
}

func (s *script) tick(on string) {
	st := s.streams[on]
	st.clock += s.rng.IntN(3)
	if s.rng.IntN(15) == 0 {
		st.clock += idleGap
	}
}

func (s *script) emit(on string, line map[string]any) {
	st := s.streams[on]
	line["timestamp"] = time.Unix(1_800_000_000+int64(st.clock), 0).UTC().Format(time.RFC3339Nano)
	st.records = append(st.records, record{at: st.clock, line: line})
}

func (s *script) ticket() string {
	if s.rng.IntN(4) == 0 {
		return ""
	}
	return fmt.Sprintf("TST-%d", 1+s.rng.IntN(3))
}

func (s *script) user(on, text string, origin string) {
	line := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
	if origin != "" {
		line["origin"] = map[string]any{"kind": origin}
	}
	s.emit(on, line)
}

func (s *script) message(on string, out int, uses ...map[string]any) {
	ref, id := s.id("req"), s.id("msg")
	s.emit(on, map[string]any{"type": "api-request", "id": ref})
	s.tick(on)
	content := []any{map[string]any{"type": "text", "text": "synthetic"}}
	for _, use := range uses {
		content = append(content, use)
	}
	usage := map[string]any{"input_tokens": 1 + s.rng.IntN(5), "output_tokens": out, "cache_read_input_tokens": s.rng.IntN(9000), "cache_creation_input_tokens": s.rng.IntN(700), "output_tokens_details": map[string]any{"thinking_tokens": s.rng.IntN(out + 1)}}
	s.emit(on, map[string]any{"type": "assistant", "requestRef": ref, "thinkingDurationMs": s.rng.IntN(900), "message": map[string]any{"id": id, "model": "synthetic-model", "content": content, "usage": usage}})
}

func (s *script) tool(on, name string, input map[string]any, failed bool, result map[string]any) string {
	id := s.id("toolu")
	s.message(on, 1+s.rng.IntN(400), map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
	s.tick(on)
	text := "synthetic output"
	if failed {
		text = "Exit code 1\nsynthetic failure"
	}
	line := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": failed, "content": text}}}}
	if result != nil {
		line["toolUseResult"] = result
	}
	s.emit(on, line)
	return id
}

func (s *script) work(on string) {
	for range 1 + s.rng.IntN(6) {
		s.tick(on)
		switch s.rng.IntN(8) {
		case 0:
			lines := []string{}
			for range s.rng.IntN(9) {
				lines = append(lines, []string{"+a", "-b", " c"}[s.rng.IntN(3)])
			}
			s.tool(on, "Edit", map[string]any{"file_path": "f" + s.id("")}, false, map[string]any{"filePath": "f1.go", "structuredPatch": []any{map[string]any{"lines": lines}}})
		case 1:
			s.tool(on, "Write", map[string]any{"file_path": "new.go"}, false, map[string]any{"type": "create", "filePath": "new" + s.id("") + ".go", "content": strings.Repeat("x\n", s.rng.IntN(5))})
		case 2:
			s.tool(on, "Bash", map[string]any{"command": "go test ./x/..."}, s.rng.IntN(2) == 0, nil)
		case 3:
			s.tool(on, "PowerShell", map[string]any{"command": "go build ./x/..."}, s.rng.IntN(3) == 0, nil)
		case 4:
			s.tool(on, "Read", map[string]any{"file_path": "r.go"}, false, nil)
		case 5:
			if ticket := s.ticket(); ticket != "" {
				s.tool(on, "Bash", map[string]any{"command": "tofu boardy show " + ticket}, false, nil)
			}
		case 6:
			if on != "" && s.rng.IntN(4) == 0 {
				s.spawn(on)
			}
		default:
			s.message(on, 1+s.rng.IntN(500))
		}
	}
}

func (s *script) spawn(parent string) {
	ticket, agent := s.ticket(), s.id("a")
	prompt := "synthetic brief " + ticket
	call := s.tool(parent, "Agent", map[string]any{"prompt": prompt, "description": "synthetic", "run_in_background": true}, false, map[string]any{"status": "async_launched", "agentId": agent})
	s.streams[agent] = &stream{clock: s.streams[parent].clock, meta: map[string]any{"agentType": "synthetic-dev", "description": "synthetic", "toolUseId": call}}
	s.agents = append(s.agents, agent)
	s.user(agent, prompt, "")
	s.work(agent)
}

func (s *script) resume(agent, ticket string) {
	lead := s.streams[""]
	if s.streams[agent].clock >= lead.clock {
		return
	}
	s.tool("", "SendMessage", map[string]any{"to": agent, "message": "synthetic follow up " + ticket}, false, map[string]any{"success": true, "resumedAgentId": agent})
	s.streams[agent].clock = lead.clock + 1
	s.user(agent, "synthetic follow up "+ticket, "")
}

func world(seed uint64) *script {
	s := newScript(seed)
	for range 3 + s.rng.IntN(4) {
		s.tick("")
		s.user("", "synthetic ask", "human")
		for range 2 + s.rng.IntN(5) {
			s.tick("")
			switch s.rng.IntN(6) {
			case 0, 1:
				s.spawn("")
			case 2:
				if len(s.agents) > 0 {
					agent := s.agents[s.rng.IntN(len(s.agents))]
					s.resume(agent, s.ticket())
					s.work(agent)
				}
			case 3:
				if s.rng.IntN(3) == 0 {
					s.emit("", map[string]any{"type": "system", "subtype": "compact_boundary"})
				}
			default:
				s.work("")
			}
		}
	}
	return s
}

func (s *script) render(t *testing.T, split int, shuffle *rand.Rand) string {
	t.Helper()
	dir := t.TempDir()
	for name, st := range s.streams {
		var lines []record
		for _, r := range st.records {
			lines = append(lines, splitLine(r, split)...)
		}
		if shuffle != nil {
			slices.SortStableFunc(lines, func(a, b record) int { return a.at - b.at })
			for start := 0; start < len(lines); {
				end := start
				for end < len(lines) && lines[end].at == lines[start].at {
					end++
				}
				shuffle.Shuffle(end-start, func(i, j int) { lines[start+i], lines[start+j] = lines[start+j], lines[start+i] })
				start = end
			}
		}
		var body strings.Builder
		for _, r := range lines {
			encoded, err := json.Marshal(r.line)
			if err != nil {
				t.Fatal(err)
			}
			body.Write(append(encoded, '\n'))
		}
		path := filepath.Join(dir, sessionID+".jsonl")
		if name != "" {
			path = filepath.Join(dir, sessionID, "subagents", "agent-"+name+".jsonl")
			write(t, strings.TrimSuffix(path, ".jsonl")+".meta.json", st.meta)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func splitLine(r record, split int) []record {
	if r.line["type"] != "assistant" || split < 2 {
		return []record{r}
	}
	message := r.line["message"].(map[string]any)
	content, usage := message["content"].([]any), message["usage"].(map[string]any)
	out := usage["output_tokens"].(int)
	parts := make([]record, split)
	for at := range split {
		line, part, spent := map[string]any{}, map[string]any{}, map[string]any{}
		for k, v := range r.line {
			line[k] = v
		}
		for k, v := range message {
			part[k] = v
		}
		for k, v := range usage {
			spent[k] = v
		}
		if at > 0 {
			delete(line, "thinkingDurationMs")
		}
		var chunk []any
		for i, block := range content {
			if i%split == at {
				chunk = append(chunk, block)
			}
		}
		spent["output_tokens"] = out * (at + 1) / split
		part["content"], part["usage"], line["message"] = chunk, spent, part
		parts[at] = record{at: r.at, line: line}
	}
	return parts
}

func write(t *testing.T, path string, value any) {
	t.Helper()
	encoded, _ := json.Marshal(value)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir string) cost.Book {
	t.Helper()
	book, err := cost.Session(dir, sessionID[:8], []string{"TST"})
	if err != nil {
		t.Fatal(err)
	}
	if total := book.BySession().Total; total.Requests == 0 || total.WallMs == 0 {
		t.Fatalf("an empty session proves nothing: %+v", total)
	}
	return book
}

func additive(f cost.Figures) cost.Figures {
	f.WallMs, f.ShareMs = 0, 0
	return f
}

func sum(rows []cost.Row) cost.Figures {
	var total cost.Figures
	for _, row := range rows {
		a, b := reflect.ValueOf(&total).Elem(), reflect.ValueOf(row.Figures)
		addInto(a, b)
	}
	return total
}

func addInto(into, from reflect.Value) {
	for i := range into.NumField() {
		field := into.Field(i)
		switch field.Kind() {
		case reflect.Struct:
			addInto(field, from.Field(i))
		case reflect.Int, reflect.Int64:
			field.SetInt(field.Int() + from.Field(i).Int())
		case reflect.Float64:
			field.SetFloat(field.Float() + from.Field(i).Float())
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func TestPartitionSumsToTheSessionTotal(t *testing.T) {
	seen := map[string]bool{}
	for seed := range uint64(seeds) {
		book := read(t, world(seed).render(t, 1, nil))
		table := book.BySession()
		for _, row := range table.Rows {
			seen[strings.SplitN(row.Key, "-", 2)[0]] = true
		}
		rows := sum(table.Rows)
		if got, want := additive(rows), additive(table.Total.Figures); got != want {
			t.Fatalf("seed %d: rows sum to %+v, total is %+v", seed, got, want)
		}
		if !near(rows.ShareMs, float64(table.Total.WallMs)) {
			t.Fatalf("seed %d: wall shares sum to %.3f ms, total wall is %d ms", seed, rows.ShareMs, table.Total.WallMs)
		}
		var forks []cost.Row
		last := 0
		for _, unit := range book.Units {
			last = max(last, unit.Generation)
		}
		for n := range last + 1 {
			forks = append(forks, book.Fork(n).BySession().Total)
		}
		if got, want := additive(sum(forks)), additive(table.Total.Figures); got != want {
			t.Fatalf("seed %d: %d forks sum to %+v, session is %+v", seed, last+1, got, want)
		}
	}
	for _, want := range []string{"TST", cost.LeadRow, cost.UnassignedRow} {
		if !seen[want] {
			t.Fatalf("no seed produced a %s row, so the partition was never exercised for it: %v", want, seen)
		}
	}
}

func TestReadingTwiceOrShuffledGivesTheSameFigures(t *testing.T) {
	for seed := range uint64(seeds) {
		s := world(seed)
		dir := s.render(t, 1, nil)
		first := read(t, dir)
		if again := read(t, dir); !reflect.DeepEqual(first.BySession(), again.BySession()) {
			t.Fatalf("seed %d: a second read differs", seed)
		}
		for round := range uint64(3) {
			shuffled := read(t, s.render(t, 1, rand.New(rand.NewPCG(seed, round))))
			if got, want := shuffled.BySession(), first.BySession(); !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d round %d: shuffling equal timestamps changed the figures\n got %+v\nwant %+v", seed, round, got.Total, want.Total)
			}
		}
	}
}

func TestSplittingAMessageKeepsItsTokens(t *testing.T) {
	for seed := range uint64(seeds) {
		s := world(seed)
		whole := read(t, s.render(t, 1, nil)).BySession()
		for _, split := range []int{2, 3, 49} {
			parts := read(t, s.render(t, split, nil)).BySession()
			if !reflect.DeepEqual(parts, whole) {
				t.Fatalf("seed %d: one message over %d lines changed the figures\n got %+v\nwant %+v", seed, split, parts.Total.Tokens, whole.Total.Tokens)
			}
		}
	}
}

func TestWallShareAndBusyTimeBoundWallTime(t *testing.T) {
	overlapped := false
	for seed := range uint64(seeds) {
		book := read(t, world(seed).render(t, 1, nil))
		tables := []cost.Table{book.BySession(), book.ForTicket("TST-1"), book.ForTicket("TST-2")}
		for _, table := range tables {
			for _, row := range append(table.Rows, table.Total) {
				wall := float64(row.WallMs)
				if row.ShareMs < 0 || row.WallMs < 0 || row.ShareMs > wall+1e-3 || row.BusyMs < row.WallMs {
					t.Fatalf("seed %d row %s %s: share %.3f, wall %d, busy %d", seed, row.Key, row.Agent, row.ShareMs, row.WallMs, row.BusyMs)
				}
			}
		}
		overlapped = overlapped || tables[0].Total.BusyMs > tables[0].Total.WallMs
	}
	if !overlapped {
		t.Fatal("no seed ran two streams at once, so the sweep was never tested")
	}
}

func TestAResumeChargesTheTicketItNames(t *testing.T) {
	s := newScript(1)
	s.user("", "synthetic ask", "human")
	s.streams[""].clock++
	call := s.id("toolu")
	s.message("", 0, map[string]any{"type": "tool_use", "id": call, "name": "Agent", "input": map[string]any{"prompt": "work on TST-1"}})
	s.streams["a1"] = &stream{clock: s.streams[""].clock, meta: map[string]any{"agentType": "synthetic-dev", "toolUseId": call}}
	s.user("a1", "work on TST-1", "")
	s.streams["a1"].clock++
	s.message("a1", 10)
	s.streams[""].clock += 30
	s.user("", "synthetic ask", "human")
	s.streams[""].clock++
	s.tool("", "SendMessage", map[string]any{"to": "a1", "message": "now TST-2"}, false, map[string]any{"resumedAgentId": "a1"})
	s.streams["a1"].clock = s.streams[""].clock + 1
	s.user("a1", "now TST-2", "")
	s.streams["a1"].clock++
	s.message("a1", 1000)
	book := read(t, s.render(t, 1, nil))
	agentOut := func(ticket string) int64 {
		var out int64
		for _, row := range book.ForTicket(ticket).Rows {
			if row.Agent == "a1" {
				out += row.Tokens.Out
			}
		}
		return out
	}
	if one, two := agentOut("TST-1"), agentOut("TST-2"); one != 10 || two != 1000 {
		t.Fatalf("agent a1 charged TST-1 %d and TST-2 %d output tokens, want 10 and 1000", one, two)
	}
	if resumes := book.ForTicket("TST-2").Total.Resumes; resumes != 1 {
		t.Fatalf("TST-2 counts %d resumes, want 1", resumes)
	}
}

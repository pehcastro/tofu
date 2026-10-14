package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type seaSpec struct {
	Module   string   `json:"module"`
	Packages []string `json:"packages"`
	LaneEach int      `json:"lane_each"`
	TestEach int      `json:"test_each"`
}

type seaPackage struct {
	Module, Pkg string
	Kinds       []string
	Max         int64
	Lane, Test  bool
}

var seaKinds = []string{"Standard", "Express", "Bulk", "Hold", "Return", "Gift", "Trial"}

var seaFiles = template.Must(template.New("sea").Funcs(template.FuncMap{"lower": strings.ToLower}).Parse(`
{{define "model.go"}}package {{.Pkg}}

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Kind int

const (
{{range $i, $k := .Kinds}}{{if eq $i 0}}	Kind{{$k}} Kind = iota
{{else}}	Kind{{$k}}
{{end}}{{end}})

func (k Kind) String() string {
	switch k {
{{range .Kinds}}	case Kind{{.}}:
		return "{{lower .}}"
{{end}}	}
	return fmt.Sprintf("kind(%d)", int(k))
}

var ErrInvalid = errors.New("{{.Pkg}}: invalid record")

type Record struct {
	ID      string    ` + "`json:\"id\"`" + `
	Owner   string    ` + "`json:\"owner\"`" + `
	Kind    Kind      ` + "`json:\"-\"`" + `
	Amount  int64     ` + "`json:\"amount\"`" + `
	Region  string    ` + "`json:\"region\"`" + `
	Tags    []string  ` + "`json:\"tags\"`" + `
	Created time.Time ` + "`json:\"created\"`" + `
}

func (r Record) Validate() error {
	if r.ID == "" || r.Owner == "" {
		return fmt.Errorf("%w: id and owner are required", ErrInvalid)
	}
	if r.Amount < 0 || r.Amount > {{.Max}} {
		return fmt.Errorf("%w: amount %d outside 0 to {{.Max}}", ErrInvalid, r.Amount)
	}
	return nil
}

func (r Record) Label() string {
	return fmt.Sprintf("{{.Pkg}}/%s/%s", r.Kind, strings.ToLower(r.ID))
}

func (r Record) HasTag(tag string) bool {
	for _, t := range r.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func (r Record) Age(now time.Time) time.Duration {
	if r.Created.IsZero() {
		return 0
	}
	return now.Sub(r.Created)
}
{{end}}
{{define "store.go"}}package {{.Pkg}}

import (
	"sort"
	"sync"
)

type Store struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewStore() *Store {
	return &Store{records: map[string]Record{}}
}

func (s *Store) Put(r Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[r.ID] = r
	return nil
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, found := s.records[id]
	return r, found
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, found := s.records[id]
	delete(s.records, id)
	return found
}

func (s *Store) Filter(keep func(Record) bool) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var kept []Record
	for _, r := range s.records {
		if keep(r) {
			kept = append(kept, r)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept
}

func (s *Store) ByOwner(owner string) []Record {
	return s.Filter(func(r Record) bool { return r.Owner == owner })
}

func (s *Store) Sum(kind Kind) int64 {
	var total int64
	for _, r := range s.Filter(func(r Record) bool { return r.Kind == kind }) {
		total += r.Amount
	}
	return total
}
{{end}}
{{define "route.go"}}package {{.Pkg}}

import (
	"strings"
{{if .Lane}}
	"{{.Module}}/intake"
{{end}})

type Router struct {
	lanes    map[Kind]string
	fallback string
}

func NewRouter() Router {
	return Router{lanes: map[Kind]string{
{{range .Kinds}}		Kind{{.}}: "{{$.Pkg}}-{{lower .}}",
{{end}}	}, fallback: "{{.Pkg}}-dead"}
}

func (r Router) Pick(rec Record) string {
	if lane, found := r.lanes[rec.Kind]; found {
		return lane
	}
	return r.fallback
}

func PriorityKind(header string) Kind {
	switch strings.ToLower(strings.TrimSpace(header)) {
	case "high", "urgent":
		return Kind{{index .Kinds 1}}
	case "low":
		return Kind{{index .Kinds 2}}
	}
	return Kind{{index .Kinds 0}}
}
{{if .Lane}}
type Lane struct {
	Prefix string
}

func (l Lane) Pick(o intake.Order) string {
	if o.Class == intake.ClassUrgent {
		return l.Prefix + "{{.Pkg}}-urgent"
	}
	return l.Prefix + "{{.Pkg}}"
}
{{end}}{{end}}
{{define "handler.go"}}package {{.Pkg}}

import (
	"encoding/json"
	"net/http"
)

type receipt struct {
	ID    string ` + "`json:\"id\"`" + `
	Queue string ` + "`json:\"queue\"`" + `
}

func Handle(s *Store) http.HandlerFunc {
	router := NewRouter()
	return func(w http.ResponseWriter, r *http.Request) {
		var rec Record
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.Kind = PriorityKind(r.Header.Get("X-Priority"))
		if err := s.Put(rec); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(receipt{ID: rec.ID, Queue: router.Pick(rec)})
	}
}
{{end}}
{{define "process.go"}}package {{.Pkg}}

import (
	"fmt"
	"sort"
	"strings"
)

type Summary struct {
	Count  map[Kind]int
	Amount map[Kind]int64
	Failed []string
}

type Stage struct {
	Limit int64
	Skip  func(Record) bool
}

func (s Stage) Run(records []Record) Summary {
	sum := Summary{Count: map[Kind]int{}, Amount: map[Kind]int64{}}
	for _, r := range records {
		if s.Skip != nil && s.Skip(r) {
			continue
		}
		if err := r.Validate(); err != nil || (s.Limit > 0 && r.Amount > s.Limit) {
			sum.Failed = append(sum.Failed, r.ID)
			continue
		}
		sum.Count[r.Kind]++
		sum.Amount[r.Kind] += r.Amount
	}
	return sum
}

func Merge(a, b Summary) Summary {
	out := Summary{Count: map[Kind]int{}, Amount: map[Kind]int64{}, Failed: append(append([]string(nil), a.Failed...), b.Failed...)}
	for _, part := range []Summary{a, b} {
		for kind, n := range part.Count {
			out.Count[kind] += n
		}
		for kind, amount := range part.Amount {
			out.Amount[kind] += amount
		}
	}
	return out
}

func (s Summary) String() string {
	kinds := make([]Kind, 0, len(s.Count))
	for kind := range s.Count {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	var b strings.Builder
	for _, kind := range kinds {
		fmt.Fprintf(&b, "{{.Pkg}} %s: %d for %d\n", kind, s.Count[kind], s.Amount[kind])
	}
	if len(s.Failed) > 0 {
		fmt.Fprintf(&b, "{{.Pkg}} failed: %s\n", strings.Join(s.Failed, ", "))
	}
	return b.String()
}
{{end}}
{{define "test.go"}}package {{.Pkg}}

import "testing"

func TestRouterAndStore(t *testing.T) {
	s := NewStore()
	if err := s.Put(Record{ID: "r1", Owner: "o", Kind: Kind{{index .Kinds 1}}, Amount: 5}); err != nil {
		t.Fatal(err)
	}
	r, found := s.Get("r1")
	if !found || NewRouter().Pick(r) != "{{.Pkg}}-{{lower (index .Kinds 1)}}" {
		t.Fatalf("record %+v found %t", r, found)
	}
	if s.Sum(Kind{{index .Kinds 1}}) != 5 {
		t.Fatal("sum")
	}
}
{{end}}`))

func writeSea(spec seaSpec, repo string) error {
	for i, name := range spec.Packages {
		p := seaPackage{Module: spec.Module, Pkg: name, Kinds: seaKinds[:3+i%5],
			Max: int64(1000 * (i + 3)), Lane: spec.LaneEach > 0 && i%spec.LaneEach == 0, Test: spec.TestEach > 0 && i%spec.TestEach == 0}
		files := []string{"model.go", "store.go", "route.go", "handler.go", "process.go"}
		if p.Test {
			files = append(files, "test.go")
		}
		dir := filepath.Join(repo, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, file := range files {
			var b bytes.Buffer
			if err := seaFiles.ExecuteTemplate(&b, file, p); err != nil {
				return err
			}
			body, err := format.Source(b.Bytes())
			if err != nil {
				return fmt.Errorf("sea %s/%s: %w", name, file, err)
			}
			if file == "test.go" {
				file = name + "_test.go"
			}
			if err := os.WriteFile(filepath.Join(dir, file), body, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

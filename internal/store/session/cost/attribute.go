package cost

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"strings"
)

type tool int

const (
	toolOther tool = iota
	toolWrite
	toolRead
	toolTest
	toolBuild
	toolSpawn
	toolMessage
)

func (rd reader) kind(c *call) tool {
	switch c.name {
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return toolWrite
	case "Read", "Grep", "Glob":
		return toolRead
	case "Agent", "Task":
		return toolSpawn
	case "SendMessage":
		return toolMessage
	case "Bash", "PowerShell":
		switch {
		case rd.test.MatchString(c.command):
			return toolTest
		case rd.build.MatchString(c.command):
			return toolBuild
		}
	}
	return toolOther
}

type switchPoint struct {
	at     int64
	rank   int
	ticket string
}

type timeline []switchPoint

func (tl timeline) at(t int64) string {
	if i := sort.Search(len(tl), func(i int) bool { return tl[i].at > t }); i > 0 {
		return tl[i-1].ticket
	}
	return ""
}

func (tl timeline) sort() {
	slices.SortFunc(tl, func(a, b switchPoint) int {
		return cmp.Or(cmp.Compare(a.at, b.at), cmp.Compare(a.rank, b.rank), strings.Compare(a.ticket, b.ticket))
	})
}

type placed struct {
	tickets    timeline
	generation int
}

type unitKey struct {
	file       *file
	ticket     string
	generation int
}

type charges struct {
	rd      reader
	lead    *file
	owner   map[string]*file
	resumes map[string][]*call
	places  map[*file]*placed
}

func (rd reader) charges(files []*file) charges {
	lead := files[0]
	slices.Sort(lead.compactions)
	ch := charges{rd: rd, lead: lead, owner: map[string]*file{}, resumes: map[string][]*call{}}
	for _, f := range files {
		for id, c := range f.calls {
			if !c.used {
				continue
			}
			ch.owner[id] = f
			if c.name == "SendMessage" && c.resumed != "" {
				ch.resumes[c.resumed] = append(ch.resumes[c.resumed], c)
			}
		}
	}
	tickets := timeline{}
	for _, p := range lead.prompts {
		tickets = append(tickets, switchPoint{at: p.at})
	}
	for _, c := range lead.calls {
		if c.used && c.named != "" {
			tickets = append(tickets, switchPoint{at: c.at, rank: 1, ticket: c.named})
		}
	}
	tickets.sort()
	ch.places = map[*file]*placed{lead: {tickets: tickets}}
	return ch
}

func (ch charges) generationAt(t int64) int {
	return sort.Search(len(ch.lead.compactions), func(i int) bool { return ch.lead.compactions[i] > t })
}

func (ch charges) place(f *file) *placed {
	if p := ch.places[f]; p != nil {
		return p
	}
	p := &placed{}
	ch.places[f] = p
	parent := ch.owner[f.meta.ToolUseID]
	var spawn *call
	if parent != nil {
		spawn = parent.calls[f.meta.ToolUseID]
	}
	ticket := cmp.Or(ch.rd.named(text(f.meta.Description)), firstPrompt(f), firstBoardy(f))
	switch {
	case spawn == nil:
		p.generation = ch.generationAt(slices.Min(append([]int64{math.MaxInt64}, f.ticks...)))
	case parent == ch.lead:
		ticket, p.generation = cmp.Or(spawn.named, ticket), ch.generationAt(spawn.at)
	default:
		above := ch.place(parent)
		ticket, p.generation = cmp.Or(spawn.named, ticket, above.tickets.at(spawn.at)), above.generation
	}
	p.tickets = timeline{{at: math.MinInt64, ticket: ticket}}
	for _, c := range ch.resumes[f.agent] {
		if c.named != "" {
			p.tickets = append(p.tickets, switchPoint{at: c.at, rank: 1, ticket: c.named})
		}
	}
	p.tickets.sort()
	return p
}

type building struct {
	Unit
	created, edited map[string]bool
}

func (rd reader) attribute(session string, files []*file) []Unit {
	ch := rd.charges(files)
	units := map[unitKey]*building{}
	for _, f := range files {
		p := ch.place(f)
		unitAt := func(t int64) *building {
			key := unitKey{file: f, ticket: p.tickets.at(t), generation: p.generation}
			if f == ch.lead {
				key.generation = ch.generationAt(t)
			}
			u := units[key]
			if u == nil {
				u = &building{Unit: Unit{Session: session, Agent: f.agent, AgentType: cmp.Or(f.meta.AgentType, LeadRow), Ticket: key.ticket, Generation: key.generation}, created: map[string]bool{}, edited: map[string]bool{}}
				units[key] = u
			}
			return u
		}
		slices.Sort(f.ticks)
		for i := 1; i < len(f.ticks); i++ {
			from, to := f.ticks[i-1], f.ticks[i]
			if gap := to - from; gap > 0 && gap <= idleCapMs {
				u := unitAt(to)
				u.spans, u.BusyMs = append(u.spans, span{from, to}), u.BusyMs+gap
			}
		}
		for _, m := range f.messages {
			u := unitAt(m.end)
			u.Requests++
			u.ThinkMs += m.think
			u.Tokens.In += m.usage.In
			u.Tokens.Out += m.usage.Out
			u.Tokens.CacheRead += m.usage.CacheRead
			u.Tokens.CacheWrite += m.usage.CacheWrite
			u.Tokens.Thinking += m.usage.Details.Thinking
			if asked, ok := f.requests[m.ref]; ok && asked <= m.end {
				u.ModelMs += m.end - asked
			}
		}
		rd.tools(f, unitAt)
	}
	out := make([]Unit, 0, len(units))
	for _, u := range units {
		u.WallMs = u.BusyMs
		u.Writes.FilesCreated, u.Writes.FilesEdited = len(u.created), len(u.edited)
		out = append(out, u.Unit)
	}
	slices.SortFunc(out, func(a, b Unit) int {
		return cmp.Or(strings.Compare(a.Agent, b.Agent), strings.Compare(a.Ticket, b.Ticket), cmp.Compare(a.Generation, b.Generation))
	})
	return out
}

func (rd reader) tools(f *file, unitAt func(int64) *building) {
	ids := make([]string, 0, len(f.calls))
	for id, c := range f.calls {
		if c.used && c.done {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(f.calls[a].end, f.calls[b].end), cmp.Compare(f.calls[a].at, f.calls[b].at), strings.Compare(a, b))
	})
	edited := false
	for _, id := range ids {
		c := f.calls[id]
		u, ms := unitAt(c.at), max(c.end-c.at, 0)
		switch rd.kind(c) {
		case toolWrite:
			edited = true
			u.Writes.LinesAdded += c.added
			u.Writes.LinesRemoved += c.removed
			switch {
			case c.file == "":
			case c.created:
				u.created[c.file] = true
			default:
				u.edited[c.file] = true
			}
		case toolRead:
			u.Reads.add(Runs{Count: 1, Ms: ms, Failed: b2i(c.isError)})
		case toolTest:
			u.Tests.add(Runs{Count: 1, Ms: ms, Failed: b2i(c.exitCode)})
			u.TestIterations += b2i(edited)
			edited = false
		case toolBuild:
			u.Builds.add(Runs{Count: 1, Ms: ms, Failed: b2i(c.exitCode)})
		case toolSpawn:
			u.Spawns++
		case toolMessage:
			u.Resumes += b2i(c.resumed != "")
		case toolOther:
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func firstPrompt(f *file) string {
	if len(f.prompts) == 0 {
		return ""
	}
	return slices.MinFunc(f.prompts, func(a, b prompt) int {
		return cmp.Or(cmp.Compare(a.at, b.at), strings.Compare(b.ticket, a.ticket))
	}).ticket
}

func firstBoardy(f *file) string {
	var named []*call
	for _, c := range f.calls {
		if c.used && c.named != "" && (c.name == "Bash" || c.name == "PowerShell") {
			named = append(named, c)
		}
	}
	if len(named) == 0 {
		return ""
	}
	return slices.MinFunc(named, func(a, b *call) int {
		return cmp.Or(cmp.Compare(a.at, b.at), strings.Compare(a.named, b.named))
	}).named
}

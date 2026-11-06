package profile

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type field struct {
	num       int
	value     uint64
	body      []byte
	delimited bool
}

func (f field) ints() []uint64 {
	if !f.delimited {
		return []uint64{f.value}
	}
	var out []uint64
	for b := f.body; len(b) > 0; {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			break
		}
		out, b = append(out, v), b[n:]
	}
	return out
}

type decoder struct{ err error }

func (d *decoder) fields(b []byte) []field {
	var out []field
	for d.err == nil && len(b) > 0 {
		key, keyed := binary.Uvarint(b)
		rest := b[max(keyed, 0):]
		f, n := field{num: int(key >> 3)}, 0
		switch key & 7 {
		case 0:
			f.value, n = binary.Uvarint(rest)
		case 1:
			n = 8
		case 2:
			length, m := binary.Uvarint(rest)
			if m > 0 && length <= uint64(len(rest)-m) {
				f.body, f.delimited, n = rest[m:m+int(length)], true, m+int(length)
			}
		case 5:
			n = 4
		}
		if keyed <= 0 || n <= 0 || n > len(rest) {
			d.err = errors.New("the pprof file is not a valid protocol buffer")
			break
		}
		out, b = append(out, f), rest[n:]
	}
	return out
}

type pprofSample struct {
	locations []uint64
	values    []uint64
}

type pprofLine struct{ function, line uint64 }

type pprofFunction struct{ name, file, start uint64 }

func readPprof(body []byte, k kind) ([]sample, unit, error) {
	if zipped, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
		if body, err = io.ReadAll(zipped); err != nil {
			return nil, 0, err
		}
	}
	var d decoder
	var text []string
	var types []uint64
	var raw []pprofSample
	locations := map[uint64][]pprofLine{}
	functions := map[uint64]pprofFunction{}
	for _, f := range d.fields(body) {
		switch f.num {
		case 1:
			for _, g := range d.fields(f.body) {
				if g.num == 1 {
					types = append(types, g.value)
				}
			}
		case 2:
			raw = append(raw, d.sample(f.body))
		case 4:
			id, lines := d.location(f.body)
			locations[id] = lines
		case 5:
			id, fn := d.function(f.body)
			functions[id] = fn
		case 6:
			text = append(text, string(f.body))
		}
	}
	if d.err != nil {
		return nil, 0, d.err
	}
	at := func(i uint64) string {
		if i < uint64(len(text)) {
			return text[i]
		}
		return ""
	}
	want, u := "cpu", inNanoseconds
	if k == mem {
		want, u = "alloc_space", inBytes
	}
	column := slices.IndexFunc(types, func(t uint64) bool { return at(t) == want })
	if column < 0 {
		return nil, 0, errors.New("the profile holds no " + want + " values")
	}
	var taken []sample
	for _, s := range raw {
		if column >= len(s.values) || s.values[column] == 0 {
			continue
		}
		var stack []frame
		for _, location := range s.locations {
			for _, l := range locations[location] {
				fn := functions[l.function]
				stack = append(stack, frame{fn: at(fn.name), file: at(fn.file), line: int(cmp.Or(fn.start, l.line))})
			}
		}
		taken = append(taken, sample{stack, int64(s.values[column])})
	}
	return taken, u, nil
}

func (d *decoder) sample(b []byte) pprofSample {
	var s pprofSample
	for _, f := range d.fields(b) {
		switch f.num {
		case 1:
			s.locations = append(s.locations, f.ints()...)
		case 2:
			s.values = append(s.values, f.ints()...)
		}
	}
	return s
}

func (d *decoder) location(b []byte) (uint64, []pprofLine) {
	var id uint64
	var lines []pprofLine
	for _, f := range d.fields(b) {
		switch f.num {
		case 1:
			id = f.value
		case 4:
			var l pprofLine
			for _, g := range d.fields(f.body) {
				switch g.num {
				case 1:
					l.function = g.value
				case 2:
					l.line = g.value
				}
			}
			lines = append(lines, l)
		}
	}
	return id, lines
}

func (d *decoder) function(b []byte) (uint64, pprofFunction) {
	var id uint64
	var fn pprofFunction
	for _, f := range d.fields(b) {
		switch f.num {
		case 1:
			id = f.value
		case 2:
			fn.name = f.value
		case 4:
			fn.file = f.value
		case 5:
			fn.start = f.value
		}
	}
	return id, fn
}

type v8Frame struct {
	FunctionName string `json:"functionName"`
	URL          string `json:"url"`
	LineNumber   int    `json:"lineNumber"`
}

func (f v8Frame) frame() frame {
	file, err := url.PathUnescape(strings.TrimPrefix(f.URL, "file://"))
	if err != nil {
		file = f.URL
	}
	if len(file) > 2 && file[0] == '/' && file[2] == ':' {
		file = file[1:]
	}
	return frame{fn: cmp.Or(f.FunctionName, "(anonymous)"), file: file, line: f.LineNumber + 1}
}

func readCPUProfile(body []byte) ([]sample, unit, error) {
	var p struct {
		Nodes []struct {
			ID        int64   `json:"id"`
			CallFrame v8Frame `json:"callFrame"`
			Children  []int64 `json:"children"`
		} `json:"nodes"`
		Samples    []int64 `json:"samples"`
		TimeDeltas []int64 `json:"timeDeltas"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, 0, fmt.Errorf("the cpuprofile is not JSON: %w", err)
	}
	parent := map[int64]int64{}
	frames := map[int64]frame{}
	for _, n := range p.Nodes {
		frames[n.ID] = n.CallFrame.frame()
		for _, child := range n.Children {
			parent[child] = n.ID
		}
	}
	var taken []sample
	for i, id := range p.Samples {
		if i >= len(p.TimeDeltas) || p.TimeDeltas[i] <= 0 || frames[id].fn == "(idle)" {
			continue
		}
		var stack []frame
		for at, more := id, true; more; at, more = parent[at] {
			if f := frames[at]; f.fn != "(root)" {
				stack = append(stack, f)
			}
		}
		taken = append(taken, sample{stack, p.TimeDeltas[i] * 1000})
	}
	return taken, inNanoseconds, nil
}

type heapNode struct {
	CallFrame v8Frame    `json:"callFrame"`
	SelfSize  int64      `json:"selfSize"`
	Children  []heapNode `json:"children"`
}

func readHeapProfile(body []byte) ([]sample, unit, error) {
	var p struct {
		Head heapNode `json:"head"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, 0, fmt.Errorf("the heapprofile is not JSON: %w", err)
	}
	var taken []sample
	var walk func(n heapNode, above []frame)
	walk = func(n heapNode, above []frame) {
		stack := above
		if f := n.CallFrame.frame(); f.fn != "(root)" {
			stack = append([]frame{f}, above...)
		}
		if n.SelfSize > 0 {
			taken = append(taken, sample{stack, n.SelfSize})
		}
		for _, child := range n.Children {
			walk(child, stack)
		}
	}
	walk(p.Head, nil)
	return taken, inBytes, nil
}

func readCollapsed(body []byte) ([]sample, unit, error) {
	var taken []sample
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		space := strings.LastIndexByte(line, ' ')
		count, err := strconv.ParseInt(line[space+1:], 10, 64)
		if space < 0 || err != nil {
			return nil, 0, fmt.Errorf("the line %q is not a stack and a count", line)
		}
		var stack []frame
		for _, part := range strings.Split(line[:space], ";") {
			stack = append(stack, collapsedFrame(part))
		}
		slices.Reverse(stack)
		taken = append(taken, sample{stack, count})
	}
	return taken, inSamples, nil
}

func collapsedFrame(part string) frame {
	open := strings.LastIndex(part, " (")
	if open < 0 || !strings.HasSuffix(part, ")") {
		return frame{fn: part}
	}
	place := part[open+2 : len(part)-1]
	colon := strings.LastIndexByte(place, ':')
	line, err := strconv.Atoi(place[colon+1:])
	if colon < 0 || err != nil {
		return frame{fn: part[:open], file: place}
	}
	return frame{fn: part[:open], file: place[:colon], line: line}
}

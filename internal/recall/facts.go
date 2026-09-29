package recall

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
)

const (
	factMark      = "fact: "
	factSourceEnd = " :: "
	factArtifact  = ", artifact "
	factSignpost  = ", it came back: "
	factBytes     = " bytes"
)

func factHead(tool, source string, bytes int) string {
	return factMark + source + factSourceEnd + tool + ", " + strconv.Itoa(bytes) + factBytes
}

func factLine(tool, source string, bytes int, handle, signpost string) string {
	line := factHead(tool, source, bytes)
	if handle != "" {
		line += factArtifact + handle
	}
	if signpost != "" {
		line += factSignpost + signpost
	}
	return line
}

func factSource(line string) string {
	source, _, _ := strings.Cut(strings.TrimPrefix(line, factMark), factSourceEnd)
	return source
}

func headOf(line string) string {
	head, _, found := strings.Cut(line, factBytes)
	if !found {
		return line
	}
	return head + factBytes
}

func factHandle(line string) string {
	_, after, found := strings.Cut(line, factArtifact)
	if !found {
		return ""
	}
	handle, _, _ := strings.Cut(after, ",")
	return handle
}

type toolArgs struct {
	Path    string `json:"path"`
	Handle  string `json:"handle"`
	Pattern string `json:"pattern"`
	Command string `json:"command"`
	URL     string `json:"url"`
}

func sourceOf(entry Entry) string {
	_, args, _ := strings.Cut(entry.SupersedeKey, " ")
	whole := oneLine(entry.SupersedeKey, konst.CarrySignpostBytes)
	var parsed toolArgs
	if json.Unmarshal([]byte(args), &parsed) != nil {
		return whole
	}
	return cmp.Or(parsed.Path, parsed.Handle, parsed.Pattern, parsed.Command, parsed.URL, shownPage(entry), whole)
}

func shownPage(entry Entry) string {
	_, args, _ := strings.Cut(entry.SupersedeKey, " ")
	var parsed struct {
		Tab *int `json:"tab"`
	}
	if json.Unmarshal([]byte(args), &parsed) != nil || parsed.Tab == nil {
		return ""
	}
	return pageURL(entry.Text)
}

func supersedeKey(entry Entry) string {
	return cmp.Or(shownPage(entry), entry.SupersedeKey)
}

const (
	untrustedOpen   = "<<<"
	untrustedBegins = " begins>>>\n"
)

func unwrapped(text string) (lead, preamble, body string) {
	before, body, found := strings.Cut(text, untrustedBegins)
	opens := strings.LastIndex(before, untrustedOpen)
	if !found || opens < 0 {
		return "", "", text
	}
	before = before[:opens]
	preambleAt := strings.LastIndex(strings.TrimSuffix(before, "\n"), "\n") + 1
	if ends := strings.LastIndex(body, "\n"+untrustedOpen); ends >= 0 {
		body = body[:ends]
	}
	return before[:preambleAt], before[preambleAt:], body
}

func pageURL(text string) string {
	_, preamble, body := unwrapped(text)
	first, _, _ := strings.Cut(body, "\n")
	for _, field := range strings.Fields(first + " " + preamble) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return strings.TrimSuffix(field, ".")
		}
	}
	return ""
}

func signpostOf(text, source string, limit int) string {
	lead, _, body := unwrapped(text)
	return oneLine(strings.Replace(lead+body, source, "", 1), limit)
}

func worthKeeping(entry Entry) bool {
	return entry.Tool != "" && entry.SupersedeKey != "" &&
		strings.TrimSpace(entry.Text) != "" && !AlreadyDropped(entry.Text)
}

func knownFacts(c Conversation) []string {
	var sheet []string
	held := make(map[string]bool)
	keep := func(line string) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, factMark) || held[line] {
			return
		}
		held[line] = true
		sheet = append(sheet, line)
	}
	for _, line := range c.Facts {
		keep(line)
	}
	for _, entry := range c.Entries {
		if entry.Tool != "" {
			continue
		}
		for _, line := range strings.Split(entry.Text, "\n") {
			keep(line)
		}
	}
	return sheet
}

func Distil(store *Store, c Conversation, signpostBytes int) ([]string, []CarriedResult, error) {
	sheet := knownFacts(c)
	at := make(map[string]int, len(sheet))
	for i, line := range sheet {
		at[factSource(line)] = i
	}
	newest := make(map[string]int, len(c.Entries))
	sources := make([]string, len(c.Entries))
	for i, entry := range c.Entries {
		if !worthKeeping(entry) {
			continue
		}
		sources[i] = sourceOf(entry)
		newest[sources[i]] = i
	}
	var kept []CarriedResult
	for i, entry := range c.Entries {
		if !worthKeeping(entry) || newest[sources[i]] != i {
			continue
		}
		source, signpost := sources[i], signpostOf(entry.Text, sources[i], signpostBytes)
		handle := entry.Handle
		standing, known := at[source]
		if known && headOf(sheet[standing]) == factHead(entry.Tool, source, len(entry.Text)) {
			handle = cmp.Or(handle, factHandle(sheet[standing]))
		} else {
			if handle == "" {
				put, err := store.put([]byte(entry.Text))
				if err != nil {
					return nil, nil, fmt.Errorf("recall: the %s result from step %d could not be kept as a fact: %w", entry.Tool, entry.Step, err)
				}
				handle = put
			}
			if known {
				sheet[standing] = ""
			}
			sheet = append(sheet, factLine(entry.Tool, source, len(entry.Text), handle, signpost))
			at[source] = len(sheet) - 1
		}
		kept = append(kept, CarriedResult{Tool: entry.Tool, Key: source, Bytes: len(entry.Text), Handle: handle, Signpost: signpost})
	}
	sheet = slices.DeleteFunc(sheet, func(line string) bool { return line == "" })
	if len(sheet) > konst.FactSheetLines {
		sheet = sheet[len(sheet)-konst.FactSheetLines:]
	}
	return sheet, kept, nil
}

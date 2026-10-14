package guard

import (
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/sys"
)

var (
	plannedDocument  = sys.LazyRegexp(`(?i)\.local/boji/(tickets|planning)/.*\.md$`)
	ticketDocument   = sys.LazyRegexp(`(?i)\.local/boji/tickets/.*\.md$`)
	scopeHeading     = sys.LazyRegexp(`(?im)^## Scope`)
	citesSource      = sys.LazyRegexp(`(?i)\.local/(sources|research)/`)
	searchedSources  = sys.LazyRegexp(`(?i)nothing in \.local/sources`)
	researchCitation = sys.LazyRegexp(`(?i)\.local/research/([A-Za-z0-9._/@-]+\.md)`)
	usedAt           = sys.LazyRegexp(`(?im)^used_at:`)
)

const researchHeadLines = 12

func researchUsed(env Env, ev event) Answer {
	path := ev.ToolInput.FilePath
	normalised := strings.ReplaceAll(path, `\`, "/")
	body := ev.ToolInput.Content
	if body == "" {
		body = ev.ToolInput.NewString
	}
	if path == "" || !plannedDocument().MatchString(normalised) || body == "" || env.Root == "" {
		return Answer{}
	}
	citing := strings.TrimSuffix(filepath.Base(normalised), filepath.Ext(normalised))
	var cited []string
	for _, match := range researchCitation().FindAllStringSubmatch(body, -1) {
		if !slices.ContainsFunc(cited, func(seen string) bool { return strings.EqualFold(seen, match[1]) }) {
			cited = append(cited, match[1])
		}
	}
	slices.SortFunc(cited, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	var stale []string
	for _, one := range cited {
		head := fileLines(filepath.Join(env.Root, ".local", "research", filepath.FromSlash(one)))
		front := strings.Join(head[:min(researchHeadLines, len(head))], "\n")
		if usedAt().MatchString(front) && !strings.Contains(strings.ToLower(front), strings.ToLower(citing)) {
			stale = append(stale, "  .local/research/"+one)
		}
	}
	if len(stale) > 0 {
		return deny(`This document cites research that does not know it was used.

` + strings.Join(stale, "\n") + `

Each of those carries used_at and planning_path at the top, and the index built
from them is the only way to see what 435 research documents have produced. A
citation written without updating the cited file puts that index back out of date.

Update each one first, in this same turn:

  used_at: "` + citing + `"            add to the list if it already names another
  planning_path: "<this file>"  when this file is a planning document

Then write this file again. Read .local/research/INDEX.md if you want to see what
the fields are for.`)
	}
	if ticketDocument().MatchString(normalised) && scopeHeading().MatchString(body) && !citesSource().MatchString(body) && !searchedSources().MatchString(body) {
		return deny(`This ticket designs something and names no source.

Twenty harnesses sit cloned in .local/sources and 435 documents in
.local/research. This has been asked for twice.

Before writing a scope, read how the harnesses already do the thing. Then either:

  cite the path, with the commit and the number measured there, or
  write the phrase "nothing in .local/sources does this" and say what you checked

The second is a real answer and it is sometimes the right one. Inventing a
mechanism without looking is not.`)
	}
	return Answer{}
}

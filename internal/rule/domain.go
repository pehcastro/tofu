package rule

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

type Document struct {
	Domain     string
	References []string
	File       string
}

type Domain struct {
	Name       string
	Rules      []Rule
	Thresholds []string
	Skills     []Document
	Agents     []Document
	References []string
	Shipped    []string
	Refused    []error
}

func (d Domain) Reaches(reference string) bool {
	return slices.Contains(d.Shipped, reference)
}

func (d Domain) Unreachable() []string {
	var missing []string
	for _, doc := range slices.Concat(d.Skills, d.Agents) {
		for _, reference := range doc.References {
			if !d.Reaches(reference) {
				missing = append(missing, fmt.Sprintf("%s names the reference %q and no domain ships it", doc.File, reference))
			}
		}
	}
	return missing
}

func (r Rule) ReachesDomain(agentDomain string, task Task) (bool, string) {
	if r.Domain != DomainQA && r.Domain != DomainDev || r.Domain == agentDomain || r.ReachesDevFor(task) {
		return true, ""
	}
	if r.AlsoReaches == ReachWorkOnTests {
		return false, fmt.Sprintf("a %s rule reaches a %s agent only when the task works on tests", r.Domain, agentDomain)
	}
	return false, fmt.Sprintf("a %s rule reaches only %s agents, and the prompt is a %s agent's", r.Domain, r.Domain, agentDomain)
}

func LoadDomains(library fs.FS, root string) ([]Domain, error) {
	byName := map[string]*Domain{}
	err := fs.WalkDir(library, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		domainName, kind, ok := placeOf(name)
		if !ok {
			return nil
		}
		data, err := fs.ReadFile(library, name)
		if err != nil {
			return err
		}
		domain, known := byName[domainName]
		if !known {
			domain = &Domain{Name: domainName}
			byName[domainName] = domain
		}
		domain.read(kind, path.Join(root, name), strings.TrimSuffix(path.Base(name), path.Ext(name)), data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(byName))
	var shipped []string
	for name, domain := range byName {
		names = append(names, name)
		shipped = append(shipped, domain.References...)
	}
	slices.Sort(names)
	out := make([]Domain, 0, len(names))
	for _, name := range names {
		domain := *byName[name]
		domain.Shipped = shipped
		out = append(out, domain)
	}
	return out, nil
}

func (d *Domain) read(kind, reported, base string, data []byte) {
	switch kind {
	case "references":
		d.References = append(d.References, base)
	case "rules":
		if declared(data, "kind") == ThresholdKind {
			d.Thresholds = append(d.Thresholds, base)
			d.refuseMismatch(reported, declared(data, "domain"))
			return
		}
		one, err := parseRule(data, reported)
		if err != nil {
			d.Refused = append(d.Refused, err)
			return
		}
		d.refuseMismatch(reported, one.Domain)
		d.Rules = append(d.Rules, one)
	case "skills", "agents":
		doc, err := parseDocument(data, reported)
		if err != nil {
			d.Refused = append(d.Refused, err)
			return
		}
		d.refuseMismatch(reported, doc.Domain)
		if kind == "skills" {
			d.Skills = append(d.Skills, doc)
			return
		}
		d.Agents = append(d.Agents, doc)
	}
}

func (d *Domain) refuseMismatch(reported, declared string) {
	if declared == "" {
		d.Refused = append(d.Refused, fmt.Errorf("%s: the file declares no domain", reported))
		return
	}
	if declared != d.Name {
		d.Refused = append(d.Refused, fmt.Errorf("%s: the file declares the domain %q and it sits in the domain %q", reported, declared, d.Name))
	}
}

func placeOf(name string) (domain, kind string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) < 3 {
		return "", "", false
	}
	domain, rest := parts[0], parts[1:]
	if domain == DomainTools {
		domain, rest = rest[0], rest[1:]
		if len(rest) < 2 {
			return "", "", false
		}
	}
	kind = rest[len(rest)-2]
	switch kind {
	case "rules", "skills", "agents", "references":
		return domain, kind, true
	}
	return "", "", false
}

func parseDocument(data []byte, reported string) (Document, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Document{}, fmt.Errorf("%s: the file opens with no front matter", reported)
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return Document{}, fmt.Errorf("%s: the front matter is never closed", reported)
	}
	doc := Document{File: reported}
	field := ""
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if item, isItem := strings.CutPrefix(line, "  - "); isItem {
			if field == "references" {
				doc.References = append(doc.References, strings.TrimSpace(item))
			}
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		field = strings.TrimSpace(key)
		if field == "domain" {
			doc.Domain = strings.TrimSpace(value)
		}
	}
	return doc, nil
}

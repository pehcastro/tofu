package rule

import (
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func shippedDomains(t *testing.T) []Domain {
	t.Helper()
	domains, err := LoadDomains(os.DirFS("../../library"), "library")
	if err != nil {
		t.Fatalf("LoadDomains: %v", err)
	}
	return domains
}

func domainNamed(t *testing.T, name string) Domain {
	t.Helper()
	for _, domain := range shippedDomains(t) {
		if domain.Name == name {
			return domain
		}
	}
	t.Fatalf("the library ships no %s domain", name)
	return Domain{}
}

func TestTheFourDomainsShipAndEachCarriesSomething(t *testing.T) {
	for _, name := range []string{DomainDev, DomainQA, DomainGeneral, "shell", "fetch"} {
		domain := domainNamed(t, name)
		if len(domain.Rules)+len(domain.Thresholds)+len(domain.Skills)+len(domain.Agents)+len(domain.References) == 0 {
			t.Fatalf("the %s domain ships nothing", name)
		}
		if len(domain.Refused) != 0 {
			t.Fatalf("the %s domain refused files: %v", name, domain.Refused)
		}
	}
}

func TestAReferenceUnderADomainIsReachableByItsSkillsAndItsAgents(t *testing.T) {
	qa := domainNamed(t, DomainQA)
	if !qa.Reaches("flakiness") {
		t.Fatalf("the qa domain ships references %v, want flakiness among them", qa.References)
	}
	reachedBySkill, reachedByAgent := false, false
	for _, skill := range qa.Skills {
		reachedBySkill = reachedBySkill || slices.Contains(skill.References, "flakiness")
	}
	for _, agent := range qa.Agents {
		reachedByAgent = reachedByAgent || slices.Contains(agent.References, "flakiness")
	}
	if !reachedBySkill || !reachedByAgent {
		t.Fatalf("flakiness is reached by a skill=%t and by an agent=%t, want both", reachedBySkill, reachedByAgent)
	}
	if unreachable := qa.Unreachable(); len(unreachable) != 0 {
		t.Fatalf("the qa domain names what it cannot reach: %v", unreachable)
	}
}

func TestAReferenceInAnotherDomainIsNotReachable(t *testing.T) {
	domains, err := LoadDomains(fstest.MapFS{
		"qa/references/flakiness.md": {Data: []byte("---\nname: flakiness\n---\n")},
		"dev/references/golang.md":   {Data: []byte("---\nname: golang\n---\n")},
		"qa/agents/qa.md":            {Data: []byte("---\nname: qa\ndomain: qa\nreferences:\n  - flakiness\n  - golang\n---\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadDomains: %v", err)
	}
	for _, domain := range domains {
		if domain.Name != DomainQA {
			continue
		}
		unreachable := domain.Unreachable()
		if len(unreachable) != 1 || !strings.Contains(unreachable[0], "golang") {
			t.Fatalf("unreachable = %v, want the dev reference alone", unreachable)
		}
		return
	}
	t.Fatal("LoadDomains found no qa domain")
}

func TestADocumentWithNoDomainIsRefusedByName(t *testing.T) {
	domains, err := LoadDomains(fstest.MapFS{
		"qa/agents/qa.md": {Data: []byte("---\nname: qa\nreferences:\n  - flakiness\n---\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadDomains: %v", err)
	}
	if len(domains) != 1 || len(domains[0].Refused) != 1 {
		t.Fatalf("domains = %+v, want one domain refusing one file", domains)
	}
	refused := domains[0].Refused[0].Error()
	if !strings.HasPrefix(refused, "library/qa/agents/qa.md") || !strings.Contains(refused, "no domain") {
		t.Fatalf("the refusal does not name the file and the field: %s", refused)
	}
}

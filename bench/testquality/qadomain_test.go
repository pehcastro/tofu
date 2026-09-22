package testquality

import (
	"os"
	"slices"
	"strings"
	"testing"

	"tofu/internal/rule"
)

func qaDomain(t *testing.T) rule.Domain {
	t.Helper()
	domains, err := rule.LoadDomains(os.DirFS("../../library"), "library")
	if err != nil {
		t.Fatalf("LoadDomains: %v", err)
	}
	for _, domain := range domains {
		if domain.Name == rule.DomainQA {
			return domain
		}
	}
	t.Fatal("the library ships no qa domain")
	return rule.Domain{}
}

func TestEveryQAAgentAndSkillNamesTheReferencesItReads(t *testing.T) {
	qa := qaDomain(t)
	if len(qa.Agents) == 0 || len(qa.Skills) == 0 {
		t.Fatalf("the qa domain ships %d agents and %d skills, want at least one of each", len(qa.Agents), len(qa.Skills))
	}
	for _, doc := range slices.Concat(qa.Agents, qa.Skills) {
		if len(doc.References) == 0 {
			t.Fatalf("%s names no reference at all", doc.File)
		}
	}
}

func TestEveryQARuleDistilledFromASkillCitesItAndCarriesEvidence(t *testing.T) {
	faults, err := QARuleFaults(os.DirFS("../../library/qa"), "library/qa")
	if err != nil {
		t.Fatalf("loading library/qa: %v", err)
	}
	if len(faults) != 0 {
		t.Fatalf("library/qa: %v", faults)
	}
}

func TestARuleThatParsesWithNoEvidenceIsAFault(t *testing.T) {
	faults, err := QARuleFaults(os.DirFS("testdata/qarules/emptyevidence"), "testdata/qarules/emptyevidence")
	if err != nil {
		t.Fatalf("QARuleFaults: %v", err)
	}
	if !slices.Equal(faults, []string{"empty_evidence carries no evidence"}) {
		t.Fatalf("faults = %v, want [empty_evidence carries no evidence]", faults)
	}
}

func TestARuleTheLoaderRejectsIsReportedWithTheLoadersReason(t *testing.T) {
	_, err := QARuleFaults(os.DirFS("testdata/qarules/rejected"), "testdata/qarules/rejected")
	if err == nil {
		t.Fatal("QARuleFaults accepted a rule the loader rejects")
	}
	if !strings.Contains(err.Error(), "a measured rule names a measurement instead") {
		t.Fatalf("err = %v, want the loader's own reason", err)
	}
}

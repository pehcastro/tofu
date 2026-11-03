package skill

import (
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestShippedReadsEverySkillFileInTheLibraryWithItsDomain(t *testing.T) {
	library := fstest.MapFS{
		"qa/general/skills/flake-triage.md": {Data: []byte("---\r\nname: flake-triage\r\ndomain: qa\r\ndescription: >\r\n  prove a test\r\n  disagrees with itself\r\n---\r\nbody\r\n")},
		"dev/skills/unnamed.md":             {Data: []byte("---\ndomain: dev\ndescription: no name given\n---\n")},
		"dev/rules/skills.yaml":             {Data: []byte("id: x\n")},
		"dev/references/skills.md":          {Data: []byte("---\ndescription: a reference\n---\n")},
	}
	got, err := Shipped(library, "library")
	if err != nil {
		t.Fatal(err)
	}
	want := []Skill{
		{Name: "flake-triage", Description: "prove a test disagrees with itself", Domain: "qa", Origin: OriginLibrary, File: "library/qa/general/skills/flake-triage.md", Dir: "library/qa/general/skills"},
		{Name: "unnamed", Description: "no name given", Domain: "dev", Origin: OriginLibrary, File: "library/dev/skills/unnamed.md", Dir: "library/dev/skills"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shipped\n got %+v\nwant %+v", got, want)
	}
}

func TestDiscoverSaysWhetherASkillCameFromTheProjectOrTheHome(t *testing.T) {
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	write(t, filepath.Join(project, ".git", "HEAD"), "ref")
	write(t, filepath.Join(project, ".claude", "skills", "near", "SKILL.md"), "---\ndescription: near\n---\n")
	write(t, filepath.Join(home, ".tofu", "skills", "mine", "SKILL.md"), "---\ndescription: mine\n---\n")
	origins := map[string]Origin{}
	for _, one := range Discover(project, home).Skills {
		origins[one.Name] = one.Origin
	}
	if want := map[string]Origin{"near": OriginProject, "mine": OriginHome}; !reflect.DeepEqual(origins, want) {
		t.Fatalf("origins %v, want %v", origins, want)
	}
}

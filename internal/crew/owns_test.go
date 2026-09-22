package crew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		owns []string
		path string
		want bool
	}{
		{
			"double_star_crosses_a_directory_boundary_owns_match_ps1_header",
			[]string{"internal/crew/**"}, "internal/crew/deep/nested/owns.go", true,
		},
		{
			"single_star_matches_within_one_segment_owns_match_ps1_header",
			[]string{"internal/crew/*.go"}, "internal/crew/owns.go", true,
		},
		{
			"single_star_does_not_cross_a_segment_boundary_owns_match_ps1_header",
			[]string{"internal/crew/*.go"}, "internal/crew/deep/owns.go", false,
		},
		{
			"a_literal_path_matches_itself_owns_match_ps1_test_ownsmatch_body",
			[]string{"cmd/tofu/main.go"}, "cmd/tofu/main.go", true,
		},
		{
			"a_literal_path_does_not_match_a_sibling_owns_match_ps1_test_ownsmatch_body",
			[]string{"cmd/tofu/main.go"}, "cmd/tofu/lint.go", false,
		},
		{
			"matching_is_case_insensitive_owns_match_ps1_tolowerinvariant",
			[]string{"Internal/Crew/**"}, "internal/crew/OWNS.GO", true,
		},
		{
			"cmd_tofu_star_star_matches_main_go_forward_slash_boji_027_acceptance_line_2",
			[]string{"cmd/tofu/**"}, "cmd/tofu/main.go", true,
		},
		{
			"cmd_tofu_star_star_matches_main_go_backslash_boji_027_acceptance_line_2",
			[]string{"cmd/tofu/**"}, `cmd\tofu\main.go`, true,
		},
		{
			"cmd_tofu_star_star_rejects_tofux_forward_slash_boji_027_acceptance_line_2",
			[]string{"cmd/tofu/**"}, "cmd/tofux/main.go", false,
		},
		{
			"cmd_tofu_star_star_rejects_tofux_backslash_boji_027_acceptance_line_2",
			[]string{"cmd/tofu/**"}, `cmd\tofux\main.go`, false,
		},
		{
			"a_subtree_glob_matches_the_directory_it_names_so_a_child_can_stand_in_it",
			[]string{"internal/crew/**"}, "internal/crew", true,
		},
		{
			"a_subtree_glob_does_not_match_a_sibling_directory",
			[]string{"internal/crew/**"}, "internal/crewx", false,
		},
		{
			"boji_006_brief_granted_bench_go_the_filed_owns_did_not_list",
			[]string{"bench/api/**", "bench/report/**", "bench/corpus/**"}, "cmd/tofu/bench.go", false,
		},
		{
			"boji_011_brief_granted_bench_go_the_filed_owns_did_not_list",
			[]string{"bench/cost/**"}, "cmd/tofu/bench.go", false,
		},
		{
			"boji_015_brief_granted_client_test_go_the_filed_owns_did_not_list",
			[]string{
				"internal/judge/question/**", "internal/konst/**",
				"internal/judge/jev/caps.go", "internal/judge/jev/wire/openrouter/**",
			}, "internal/judge/jev/client_test.go", false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Matches(c.path, c.owns)
			if err != nil {
				t.Fatalf("Matches(%q, %v) returned error: %v", c.path, c.owns, err)
			}
			if got != c.want {
				t.Errorf("Matches(%q, %v) = %v, want %v", c.path, c.owns, got, c.want)
			}
		})
	}
}

func TestMatchesRefusesAnUnparseableGlobRatherThanSilentlyNotMatching(t *testing.T) {
	_, err := Matches("internal/crew/owns.go", []string{"internal/crew/??.go"})
	if err == nil {
		t.Fatal("Matches returned no error for an unparseable glob")
	}
	var target UnparseableGlobError
	if !errors.As(err, &target) {
		t.Fatalf("Matches returned %v, want an UnparseableGlobError", err)
	}
}

func TestMatchesRefusesAPathItCannotReadRatherThanDecidingOnIt(t *testing.T) {
	for _, path := range []string{"", "   ", "internal/*/owns.go", "internal/crew/../../etc/passwd"} {
		t.Run(path, func(t *testing.T) {
			matched, err := Matches(path, []string{"internal/crew/**"})
			if matched {
				t.Fatalf("Matches(%q) allowed the write", path)
			}
			var target UnparseablePathError
			if !errors.As(err, &target) {
				t.Fatalf("Matches(%q) returned %v, want an UnparseablePathError", path, err)
			}
		})
	}
}

func TestASymlinkWhoseLastComponentLeavesTheTreeIsRefused(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	held := filepath.Join(root, "internal", "crew")
	if err := os.MkdirAll(held, 0o750); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(secret, []byte("package outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(held, "owns.go"), []byte("package crew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(held, "borrowed.go")); err != nil {
		t.Skipf("this host will not create a symlink without elevation, so the escape cannot be built: %v", err)
	}
	t.Chdir(root)

	if _, err := Matches("internal/crew/owns.go", []string{"internal/crew/**"}); err != nil {
		t.Fatalf("a real file inside owns was refused: %v", err)
	}
	matched, err := Matches("internal/crew/borrowed.go", []string{"internal/crew/**"})
	if matched {
		t.Fatal("a symlink pointing outside the tree was allowed by its own name")
	}
	var target EscapingPathError
	if !errors.As(err, &target) {
		t.Fatalf("Matches returned %v, want an EscapingPathError", err)
	}
}

func TestASubtreeGlobDoesNotMatchASiblingSharingItsPrefix(t *testing.T) {
	for _, path := range []string{"src-old/main.go", "src-old"} {
		matched, err := Matches(path, []string{"src/**"})
		if err != nil {
			t.Fatalf("Matches(%q) returned error: %v", path, err)
		}
		if matched {
			t.Fatalf("src/** matched %q", path)
		}
	}
	matched, err := Matches("src/main.go", []string{"src/**"})
	if err != nil || !matched {
		t.Fatalf("src/** did not match its own child: %v %v", matched, err)
	}
}

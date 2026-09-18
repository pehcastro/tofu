package crew

import (
	"errors"
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
			[]string{"cmd/boji/main.go"}, "cmd/boji/main.go", true,
		},
		{
			"a_literal_path_does_not_match_a_sibling_owns_match_ps1_test_ownsmatch_body",
			[]string{"cmd/boji/main.go"}, "cmd/boji/lint.go", false,
		},
		{
			"matching_is_case_insensitive_owns_match_ps1_tolowerinvariant",
			[]string{"Internal/Crew/**"}, "internal/crew/OWNS.GO", true,
		},
		{
			"cmd_boji_star_star_matches_main_go_forward_slash_boji_027_acceptance_line_2",
			[]string{"cmd/boji/**"}, "cmd/boji/main.go", true,
		},
		{
			"cmd_boji_star_star_matches_main_go_backslash_boji_027_acceptance_line_2",
			[]string{"cmd/boji/**"}, `cmd\boji\main.go`, true,
		},
		{
			"cmd_boji_star_star_rejects_bojix_forward_slash_boji_027_acceptance_line_2",
			[]string{"cmd/boji/**"}, "cmd/bojix/main.go", false,
		},
		{
			"cmd_boji_star_star_rejects_bojix_backslash_boji_027_acceptance_line_2",
			[]string{"cmd/boji/**"}, `cmd\bojix\main.go`, false,
		},
		{
			"boji_006_brief_granted_bench_go_the_filed_owns_did_not_list",
			[]string{"bench/api/**", "bench/report/**", "bench/corpus/**"}, "cmd/boji/bench.go", false,
		},
		{
			"boji_011_brief_granted_bench_go_the_filed_owns_did_not_list",
			[]string{"bench/cost/**"}, "cmd/boji/bench.go", false,
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

func TestOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{
			"two_lists_sharing_an_exact_path_intersect",
			[]string{"cmd/boji/main.go"}, []string{"cmd/boji/main.go"}, true,
		},
		{
			"two_lists_on_disjoint_directories_do_not_intersect",
			[]string{"internal/crew/**"}, []string{"internal/judge/**"}, false,
		},
		{
			"a_star_star_and_a_literal_file_under_it_intersect",
			[]string{"internal/crew/**"}, []string{"internal/crew/owns.go"}, true,
		},
		{
			"a_literal_file_and_a_star_star_over_it_intersect_reversed",
			[]string{"internal/crew/owns.go"}, []string{"internal/crew/**"}, true,
		},
		{
			"disjoint_siblings_do_not_intersect_even_with_star_star",
			[]string{"cmd/boji/**"}, []string{"cmd/bojix/**"}, false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Overlap(c.a, c.b)
			if err != nil {
				t.Fatalf("Overlap(%v, %v) returned error: %v", c.a, c.b, err)
			}
			if got != c.want {
				t.Errorf("Overlap(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
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

func TestOverlapRefusesAnUnparseableGlobRatherThanSilentlyNotMatching(t *testing.T) {
	_, err := Overlap([]string{"internal/crew/??.go"}, []string{"internal/crew/owns.go"})
	if err == nil {
		t.Fatal("Overlap returned no error for an unparseable glob")
	}
	var target UnparseableGlobError
	if !errors.As(err, &target) {
		t.Fatalf("Overlap returned %v, want an UnparseableGlobError", err)
	}
}

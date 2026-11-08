//go:build !grammar_subset || grammar_subset_c

package grammarruntime

func RegisterCSupport() {
	RegisterExternalScanner("c", PreprocExternalScanner{never: preprocNever{"0", "__cplusplus"}})
}

//go:build !grammar_subset || grammar_subset_objc

package grammarruntime

func RegisterObjcSupport() {
	RegisterExternalScanner("objc", PreprocExternalScanner{never: preprocNever{"0", "__cplusplus"}})
}

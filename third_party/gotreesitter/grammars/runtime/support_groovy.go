//go:build !grammar_subset || grammar_subset_groovy

package grammarruntime

func RegisterGroovySupport() {
	RegisterExternalScanner("groovy", GroovyExternalScanner{})
}

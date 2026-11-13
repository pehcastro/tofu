//go:build !grammar_subset || grammar_subset_erlang

package erlang

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "3a95d33d9a72ade08150f47f15e6eb182296f4756cb879dda741e4dfbd12f454"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.ErlangC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.ErlangExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterErlangSupport()
		grammarruntime.RegisterBlob("erlang", grammarblobs.Erlang)
		gotreesitter.RegisterCCoreLanguage("erlang", LanguageC)
	})
	return grammarruntime.Language("erlang.bin")
}

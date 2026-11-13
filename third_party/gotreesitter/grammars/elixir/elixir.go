//go:build !grammar_subset || grammar_subset_elixir

package elixir

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "a9b075c62ed19db08fe7f917651b6428f1f288f5f8a24631773d07dd9b54fe32"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.ElixirC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.ElixirExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterElixirSupport()
		grammarruntime.RegisterBlob("elixir", grammarblobs.Elixir)
		gotreesitter.RegisterCCoreLanguage("elixir", LanguageC)
	})
	return grammarruntime.Language("elixir.bin")
}

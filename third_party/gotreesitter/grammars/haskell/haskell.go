//go:build !grammar_subset || grammar_subset_haskell

package haskell

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "d970c6f50374c3a7420ad6d2c7487eb16d65cad02141be198a255c8d03c386fa"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.HaskellC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.HaskellExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterHaskellSupport()
		grammarruntime.RegisterBlob("haskell", grammarblobs.Haskell)
		gotreesitter.RegisterCCoreLanguage("haskell", LanguageC)
	})
	return grammarruntime.Language("haskell.bin")
}

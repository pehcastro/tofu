//go:build !grammar_subset || grammar_subset_c

package c

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "86623a5dd307d29fea1383a2120d572e4c15974f92cc0ccbb0a1823b16f307c7"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.CC())
	if err != nil {
		return nil
	}
	bound, ok := grammarruntime.LookupExternalScanner("c").(interface {
		ExternalScannerForLanguage(*gotreesitter.Language) gotreesitter.ExternalScanner
	})
	if !ok {
		return nil
	}
	lang.ExternalScanner = bound.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterCSupport()
		grammarruntime.RegisterBlob("c", grammarblobs.C)
		gotreesitter.RegisterCCoreLanguage("c", LanguageC)
	})
	return grammarruntime.Language("c.bin")
}

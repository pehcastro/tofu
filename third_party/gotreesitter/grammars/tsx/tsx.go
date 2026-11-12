//go:build !grammar_subset || grammar_subset_tsx

package tsx

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "2a28c6057addaca38d7922d3e646ee3165c5de10b71bcdb73b6d951892194abb"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.TsxC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.TsxExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterTsxSupport()
		grammarruntime.RegisterBlob("tsx", grammarblobs.Tsx)
		gotreesitter.RegisterCCoreLanguage("tsx", LanguageC)
	})
	return grammarruntime.Language("tsx.bin")
}

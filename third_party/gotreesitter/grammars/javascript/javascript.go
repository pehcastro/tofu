//go:build !grammar_subset || grammar_subset_javascript

package javascript

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "142deed2ea9b34abfedda0343336820841a52168ed52195f2363540436cb9f6a"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.JavascriptC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.JavaScriptExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterJavascriptSupport()
		grammarruntime.RegisterBlob("javascript", grammarblobs.Javascript)
		gotreesitter.RegisterCCoreLanguage("javascript", LanguageC)
	})
	return grammarruntime.Language("javascript.bin")
}

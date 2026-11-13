//go:build !grammar_subset || grammar_subset_html

package html

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "c5da98a34f94a0da7b825015c3e3c018d57ea2f6ad7d0d61c13ac895f83d1521"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.HtmlC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.HTMLExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterHtmlSupport()
		grammarruntime.RegisterBlob("html", grammarblobs.Html)
		gotreesitter.RegisterCCoreLanguage("html", LanguageC)
	})
	return grammarruntime.Language("html.bin")
}

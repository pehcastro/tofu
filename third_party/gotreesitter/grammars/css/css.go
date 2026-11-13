//go:build !grammar_subset || grammar_subset_css

package css

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "84ac039b57506327c20d79c5690ea6b72c8a1f9d6f51345f829c141b969991e8"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.CssC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.CssExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterCssSupport()
		grammarruntime.RegisterBlob("css", grammarblobs.Css)
		gotreesitter.RegisterCCoreLanguage("css", LanguageC)
	})
	return grammarruntime.Language("css.bin")
}

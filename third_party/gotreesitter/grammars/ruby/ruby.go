//go:build !grammar_subset || grammar_subset_ruby

package ruby

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "ce0dd76cd817d59b29a2d7e42e22cacfa488fb24cb0ae02cbfb4e7e238b201f5"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.RubyC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.RubyExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterRubySupport()
		grammarruntime.RegisterBlob("ruby", grammarblobs.Ruby)
		gotreesitter.RegisterCCoreLanguage("ruby", LanguageC)
	})
	return grammarruntime.Language("ruby.bin")
}

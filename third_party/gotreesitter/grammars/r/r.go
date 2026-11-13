//go:build !grammar_subset || grammar_subset_r

package r

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "fc946c48e7bb7a447fb6568a371d7e67fcb4785575010f78c3f8695e3d3b75af"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.RC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.RExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterRSupport()
		grammarruntime.RegisterBlob("r", grammarblobs.R)
		gotreesitter.RegisterCCoreLanguage("r", LanguageC)
	})
	return grammarruntime.Language("r.bin")
}

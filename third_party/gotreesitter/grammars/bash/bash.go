//go:build !grammar_subset || grammar_subset_bash

package bash

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "faedfea75850621b951efb64c79ba37da6c1eae1733c39d6c1313010b19fd3ba"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.BashC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.BashExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterBashSupport()
		grammarruntime.RegisterBlob("bash", grammarblobs.Bash)
		gotreesitter.RegisterCCoreLanguage("bash", LanguageC)
	})
	return grammarruntime.Language("bash.bin")
}

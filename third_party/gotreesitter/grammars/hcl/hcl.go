//go:build !grammar_subset || grammar_subset_hcl

package hcl

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "6dccc9935c8ef0a9f70abc1e075f2854b513103ebaf750c07380fd2ffa13dc76"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.HclC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.HclExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterHclSupport()
		grammarruntime.RegisterBlob("hcl", grammarblobs.Hcl)
		gotreesitter.RegisterCCoreLanguage("hcl", LanguageC)
	})
	return grammarruntime.Language("hcl.bin")
}

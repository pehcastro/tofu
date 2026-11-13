//go:build !grammar_subset || grammar_subset_objc

package objc

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "008da09a09663534188077735ea79ab1d03cec13e628f61d149d82b4909b0d36"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.ObjcC())
	if err != nil {
		return nil
	}
	bound, ok := grammarruntime.LookupExternalScanner("objc").(interface {
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
		grammarruntime.RegisterObjcSupport()
		grammarruntime.RegisterBlob("objc", grammarblobs.Objc)
		gotreesitter.RegisterCCoreLanguage("objc", LanguageC)
	})
	return grammarruntime.Language("objc.bin")
}

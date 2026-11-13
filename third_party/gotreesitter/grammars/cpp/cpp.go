//go:build !grammar_subset || grammar_subset_cpp

package cpp

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "1034fff95a7b64ab6deaedf7cf012eccd841ff4f9479baea66e1591a63e48866"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.CppC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.CppExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterCppSupport()
		grammarruntime.RegisterBlob("cpp", grammarblobs.Cpp)
		gotreesitter.RegisterCCoreLanguage("cpp", LanguageC)
	})
	return grammarruntime.Language("cpp.bin")
}

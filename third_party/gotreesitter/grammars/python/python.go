//go:build !grammar_subset || grammar_subset_python

package python

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "5a733769bfdde8dc091c7b10e3794d37a621257a4f6d98e8a8caa9b00fac355f"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.PythonC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.PythonExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterPythonSupport()
		grammarruntime.RegisterBlob("python", grammarblobs.Python)
		gotreesitter.RegisterCCoreLanguage("python", LanguageC)
	})
	return grammarruntime.Language("python.bin")
}

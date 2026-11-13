//go:build !grammar_subset || grammar_subset_powershell

package powershell

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "19c495e54f1ac55c494a70d194da06274b475b2679dd379cc3f092377041ab1b"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.PowershellC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.PowershellExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterPowershellSupport()
		grammarruntime.RegisterBlob("powershell", grammarblobs.Powershell)
		gotreesitter.RegisterCCoreLanguage("powershell", LanguageC)
	})
	return grammarruntime.Language("powershell.bin")
}

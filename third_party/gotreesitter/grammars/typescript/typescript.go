//go:build !grammar_subset || grammar_subset_typescript

package typescript

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "a18959c330d38e4cdc20876561b94590cbf86b744377828614719b078fc904b7"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.TypescriptC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.TypeScriptExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterTypescriptSupport()
		grammarruntime.RegisterBlob("typescript", grammarblobs.Typescript)
		gotreesitter.RegisterCCoreLanguage("typescript", LanguageC)
	})
	return grammarruntime.Language("typescript.bin")
}

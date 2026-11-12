//go:build !grammar_subset || grammar_subset_rust

package rust

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "175dc35bc305306f1e78d175197e181cd3bf3d6cb064b532b8acbb0b1a8b730f"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.RustC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.RustExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterRustSupport()
		grammarruntime.RegisterBlob("rust", grammarblobs.Rust)
		gotreesitter.RegisterCCoreLanguage("rust", LanguageC)
	})
	return grammarruntime.Language("rust.bin")
}

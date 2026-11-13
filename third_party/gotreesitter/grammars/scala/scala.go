//go:build !grammar_subset || grammar_subset_scala

package scala

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "ad5a127bf03147406c53ae9b8b5a94011c2e18ad5e8ffa988b07ce3ec856cddb"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.ScalaC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.ScalaExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterScalaSupport()
		grammarruntime.RegisterBlob("scala", grammarblobs.Scala)
		gotreesitter.RegisterCCoreLanguage("scala", LanguageC)
	})
	return grammarruntime.Language("scala.bin")
}

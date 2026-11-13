//go:build !grammar_subset || grammar_subset_clojure

package clojure

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "21f5731e530505f7489f3e8f75839ba07522a1275a168799a0e354544291507d"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.ClojureC())
	if err != nil {
		return nil
	}
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterBlob("clojure", grammarblobs.Clojure)
		gotreesitter.RegisterCCoreLanguage("clojure", LanguageC)
	})
	return grammarruntime.Language("clojure.bin")
}

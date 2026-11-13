//go:build !grammar_subset || grammar_subset_json

package json

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "3bc72e1bbe6cd750cc07ed8c8231d5b2b47a3a00a6269bbd5c1ec08921033054"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.JsonC())
	if err != nil {
		return nil
	}
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterBlob("json", grammarblobs.Json)
		gotreesitter.RegisterCCoreLanguage("json", LanguageC)
	})
	return grammarruntime.Language("json.bin")
}

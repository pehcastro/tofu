//go:build !grammar_subset || grammar_subset_go

package golang

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "443b3163737165f6b4a6212d00c74b6a6a38420d83fa6102d887257a78964ffd"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.GoC())
	if err != nil {
		return nil
	}
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterGoSupport()
		grammarruntime.RegisterBlob("go", grammarblobs.Go)
		gotreesitter.RegisterCCoreLanguage("go", LanguageC)
	})
	return grammarruntime.Language("go.bin")
}

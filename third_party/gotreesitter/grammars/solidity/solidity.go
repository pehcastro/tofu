//go:build !grammar_subset || grammar_subset_solidity

package solidity

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "bc7d3f824216be90d0cf7f7595b75566b1d82fdbec883c53a5b1114eb3d4bbee"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.SolidityC())
	if err != nil {
		return nil
	}
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterBlob("solidity", grammarblobs.Solidity)
		gotreesitter.RegisterCCoreLanguage("solidity", LanguageC)
	})
	return grammarruntime.Language("solidity.bin")
}

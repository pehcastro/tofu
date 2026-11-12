//go:build !grammar_subset || grammar_subset_toml

package toml

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "4b92e82a1f5680cb9a9704852c0583721525a756c84b2559e9e59095ee2ceca5"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterTomlSupport()
		grammarruntime.RegisterBlob("toml", grammarblobs.Toml)
	})
	return grammarruntime.Language("toml.bin")
}

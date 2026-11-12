//go:build !grammar_subset || grammar_subset_elixir

package elixir

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "a9b075c62ed19db08fe7f917651b6428f1f288f5f8a24631773d07dd9b54fe32"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterElixirSupport()
		grammarruntime.RegisterBlob("elixir", grammarblobs.Elixir)
	})
	return grammarruntime.Language("elixir.bin")
}

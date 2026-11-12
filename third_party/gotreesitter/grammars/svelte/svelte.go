//go:build !grammar_subset || grammar_subset_svelte

package svelte

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "24dd3660a60368168e639dd43f45fe70a1a6569b2b1d0aac024947bb326b466e"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterSvelteSupport()
		grammarruntime.RegisterBlob("svelte", grammarblobs.Svelte)
	})
	return grammarruntime.Language("svelte.bin")
}

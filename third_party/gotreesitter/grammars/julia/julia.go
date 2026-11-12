//go:build !grammar_subset || grammar_subset_julia

package julia

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "b1a858db041f998f03996a71ee1e3994d5d17f64812ca9c560e9d9e54cd7fc1b"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterJuliaSupport()
		grammarruntime.RegisterBlob("julia", grammarblobs.Julia)
	})
	return grammarruntime.Language("julia.bin")
}

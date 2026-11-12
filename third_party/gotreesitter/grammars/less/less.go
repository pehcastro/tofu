//go:build !grammar_subset || grammar_subset_less

package less

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "f0c13da109eab8dd41859caf5965d42cb121a8420bd0451c92477bd47cccbf7e"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterLessSupport()
		grammarruntime.RegisterBlob("less", grammarblobs.Less)
	})
	return grammarruntime.Language("less.bin")
}

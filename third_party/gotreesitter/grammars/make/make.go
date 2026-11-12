//go:build !grammar_subset || grammar_subset_make

package make

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "5314a6f18becfc5471e5176b20565ab78028c83a20b6327c009520f36c3fe640"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() { grammarruntime.RegisterBlob("make", grammarblobs.Make) })
	return grammarruntime.Language("make.bin")
}

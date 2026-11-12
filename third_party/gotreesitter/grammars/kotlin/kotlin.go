//go:build !grammar_subset || grammar_subset_kotlin

package kotlin

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "3750423366923e24a4ff74c7b0999f65d20aeb6499c875867feff9fb4c317f95"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterKotlinSupport()
		grammarruntime.RegisterBlob("kotlin", grammarblobs.Kotlin)
	})
	return grammarruntime.Language("kotlin.bin")
}

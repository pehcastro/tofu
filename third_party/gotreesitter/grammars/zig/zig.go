//go:build !grammar_subset || grammar_subset_zig

package zig

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "e47329510284ba1f5d2e63ae2c48bf9a70c4705c6355c34c9b315a2edaa4f4fa"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() { grammarruntime.RegisterBlob("zig", grammarblobs.Zig) })
	return grammarruntime.Language("zig.bin")
}

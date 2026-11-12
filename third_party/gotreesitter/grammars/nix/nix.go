//go:build !grammar_subset || grammar_subset_nix

package nix

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "11500c48783d7b18c88979a8dddf24b6670ace1f7f1ae209b998d1508b8b72ed"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterNixSupport()
		grammarruntime.RegisterBlob("nix", grammarblobs.Nix)
	})
	return grammarruntime.Language("nix.bin")
}

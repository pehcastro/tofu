//go:build !grammar_subset || grammar_subset_c_sharp

package c_sharp

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "63543a55f1aa73865b6d4786a817a972c010faaea8e29397e3afd7da4bc98056"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterC_sharpSupport()
		grammarruntime.RegisterBlob("c_sharp", grammarblobs.C_sharp)
	})
	return grammarruntime.Language("c_sharp.bin")
}

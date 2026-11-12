//go:build !grammar_subset || grammar_subset_powershell

package powershell

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "19c495e54f1ac55c494a70d194da06274b475b2679dd379cc3f092377041ab1b"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterPowershellSupport()
		grammarruntime.RegisterBlob("powershell", grammarblobs.Powershell)
	})
	return grammarruntime.Language("powershell.bin")
}

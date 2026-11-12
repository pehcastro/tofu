//go:build !grammar_subset || grammar_subset_swift

package swift

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "ae63250035dcd60817256549b88e21e4bb8893f1ac522d2dd37cea6148bf7da2"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterSwiftSupport()
		grammarruntime.RegisterBlob("swift", grammarblobs.Swift)
	})
	return grammarruntime.Language("swift.bin")
}

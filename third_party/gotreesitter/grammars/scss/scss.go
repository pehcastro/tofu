//go:build !grammar_subset || grammar_subset_scss

package scss

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "9aac8551f83f7cd5eb96f0d8501f0048c256fc519cdd63e92e590bbb25ce6e53"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterScssSupport()
		grammarruntime.RegisterBlob("scss", grammarblobs.Scss)
	})
	return grammarruntime.Language("scss.bin")
}

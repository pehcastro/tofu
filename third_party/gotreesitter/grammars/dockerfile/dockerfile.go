//go:build !grammar_subset || grammar_subset_dockerfile

package dockerfile

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "a9a89624d818fef06ee16225af7683390f6b4e906e1fab60db9f54a0f51b7843"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterDockerfileSupport()
		grammarruntime.RegisterBlob("dockerfile", grammarblobs.Dockerfile)
	})
	return grammarruntime.Language("dockerfile.bin")
}

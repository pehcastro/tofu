//go:build !grammar_subset || grammar_subset_cmake

package cmake

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "ba2974144023195ccf657ef21b9a73905b705a5335a946de8db32971209bd96e"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterCmakeSupport()
		grammarruntime.RegisterBlob("cmake", grammarblobs.Cmake)
	})
	return grammarruntime.Language("cmake.bin")
}

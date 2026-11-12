//go:build !grammar_subset || grammar_subset_vue

package vue

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "9cf4e6d67af2f0a310894c546c7ed73c9a35976058a9a31b967a693136683280"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterVueSupport()
		grammarruntime.RegisterBlob("vue", grammarblobs.Vue)
	})
	return grammarruntime.Language("vue.bin")
}

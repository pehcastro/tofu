//go:build !grammar_subset || grammar_subset_java

package java

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "3b558988e13a76cf3bf869dfc7021faafca8339018a75538023e250ce4615961"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() { grammarruntime.RegisterBlob("java", grammarblobs.Java) })
	return grammarruntime.Language("java.bin")
}

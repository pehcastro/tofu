//go:build !grammar_subset || grammar_subset_scala

package scala

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "ad5a127bf03147406c53ae9b8b5a94011c2e18ad5e8ffa988b07ce3ec856cddb"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterScalaSupport()
		grammarruntime.RegisterBlob("scala", grammarblobs.Scala)
	})
	return grammarruntime.Language("scala.bin")
}

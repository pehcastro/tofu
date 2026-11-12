//go:build !grammar_subset || grammar_subset_dart

package dart

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "1430af6490aa31001fed77393c4c006230a0511e6bb99d54f36a3c2c42de6bb6"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterDartSupport()
		grammarruntime.RegisterBlob("dart", grammarblobs.Dart)
	})
	return grammarruntime.Language("dart.bin")
}

//go:build !grammar_subset || grammar_subset_php

package php

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "0b845965168c9233628914be929202e0e5cb767a3574a0409858e830ff0e4937"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterPhpSupport()
		grammarruntime.RegisterBlob("php", grammarblobs.Php)
	})
	return grammarruntime.Language("php.bin")
}

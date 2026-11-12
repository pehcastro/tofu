//go:build !grammar_subset || grammar_subset_proto

package proto

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "9c697fdd149b7b3fc2ccd49212efa35eba9a6eadd185e1b6635f568f62185612"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() { grammarruntime.RegisterBlob("proto", grammarblobs.Proto) })
	return grammarruntime.Language("proto.bin")
}

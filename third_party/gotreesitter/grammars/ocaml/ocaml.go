//go:build !grammar_subset || grammar_subset_ocaml

package ocaml

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "97cf081c12b23db66048e08fd84d5d7f44868072d3667bcb892fb66f477c0c46"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterOcamlSupport()
		grammarruntime.RegisterBlob("ocaml", grammarblobs.Ocaml)
	})
	return grammarruntime.Language("ocaml.bin")
}

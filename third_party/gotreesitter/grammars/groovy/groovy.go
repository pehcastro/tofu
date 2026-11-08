//go:build !grammar_subset || grammar_subset_groovy

package groovy

import (
	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "5d5b91aa091509015b0a62a61f44e5fa7a4b42a5bbb02ca34bcce7e0e0c8ce4b"

func init() {
	grammarruntime.RegisterGroovySupport()
	grammarruntime.RegisterBlob("groovy", grammarblobs.Groovy)
}

func Language() *gotreesitter.Language { return grammarruntime.Language("groovy.bin") }

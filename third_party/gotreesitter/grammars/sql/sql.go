//go:build !grammar_subset || grammar_subset_sql

package sql

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "3ddd3e18f1759af0a6d5f151ca51f5013bc7195b2af3e8a0b502f1e112e99b48"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterSqlSupport()
		grammarruntime.RegisterBlob("sql", grammarblobs.Sql)
	})
	return grammarruntime.Language("sql.bin")
}

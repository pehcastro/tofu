//go:build !grammar_subset || grammar_subset_lua

package lua

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "6e61836b75d4de07ae78d38f3f79ae969f6705036f20be274c952cb09efd4657"

var registered sync.Once

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterLuaSupport()
		grammarruntime.RegisterBlob("lua", grammarblobs.Lua)
	})
	return grammarruntime.Language("lua.bin")
}

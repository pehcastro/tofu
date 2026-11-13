//go:build !grammar_subset || grammar_subset_yaml

package yaml

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "fa20b960e09b76248085e2a3a8a1e3e7b2a655e0679a32843b5aa49551f11dd7"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.YamlC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.YamlExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterYamlSupport()
		grammarruntime.RegisterBlob("yaml", grammarblobs.Yaml)
		gotreesitter.RegisterCCoreLanguage("yaml", LanguageC)
	})
	return grammarruntime.Language("yaml.bin")
}

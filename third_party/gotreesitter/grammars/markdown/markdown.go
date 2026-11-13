//go:build !grammar_subset || grammar_subset_markdown

package markdown

import (
	"sync"

	"github.com/odvcencio/gotreesitter"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "164af09376cb8f8f9a77e84fd169a63894c7a9da476b24d631eb54b19197c84a"

var registered sync.Once

func LanguageC() *gotreesitter.Language {
	lang, err := gotreesitter.LoadLanguage(grammarblobs.MarkdownC())
	if err != nil {
		return nil
	}
	lang.ExternalScanner = grammarruntime.MarkdownExternalScanner{}.ExternalScannerForLanguage(lang)
	return lang
}

func Language() *gotreesitter.Language {
	registered.Do(func() {
		grammarruntime.RegisterMarkdownSupport()
		grammarruntime.RegisterBlob("markdown", grammarblobs.Markdown)
		gotreesitter.RegisterCCoreLanguage("markdown", LanguageC)
	})
	return grammarruntime.Language("markdown.bin")
}

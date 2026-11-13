package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

var registeredScanner = regexp.MustCompile(`RegisterExternalScanner\("[^"]+", (\w+)\{\}\)`)

func main() {
	check := flag.Bool("check", false, "name every generated file that differs without writing it")
	flag.Parse()
	if err := generate(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(check bool) error {
	blobs, err := filepath.Glob("grammars/grammar_blobs/*.bin")
	if err != nil {
		return err
	}
	if len(blobs) == 0 {
		return errors.New("no grammar blobs found; run from the module root")
	}
	var packages, differ int
	for _, blob := range blobs {
		name := strings.TrimSuffix(filepath.Base(blob), ".bin")
		if base := strings.TrimSuffix(name, "_c"); base != name && exists("grammars/grammar_blobs/"+base+".bin") {
			continue
		}
		data, err := os.ReadFile(blob)
		if err != nil {
			return err
		}
		ccore, scanner, err := cCore(name)
		if err != nil {
			return err
		}
		source, err := wrapper(name, data, exists("grammars/runtime/support_"+name+".go"), ccore, scanner)
		if err != nil {
			return fmt.Errorf("format %s: %w", name, err)
		}
		packages++
		files := map[string][]byte{filepath.Join("grammars", name, name+".go"): source}
		if ccore {
			files[filepath.Join("grammars", "grammar_blobs", name+"_c.go")] = cAccessor(name)
		}
		for path, source := range files {
			if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, source) {
				continue
			}
			differ++
			if check {
				fmt.Println("differs:", path)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, source, 0o644); err != nil {
				return err
			}
		}
	}
	fmt.Printf("%d grammar packages, %d differed\n", packages, differ)
	if check && differ > 0 {
		return errors.New("generated files differ")
	}
	return nil
}

func cCore(name string) (ccore bool, scanner string, err error) {
	blob, err := os.ReadFile("grammars/grammar_blobs/" + name + "_c.bin")
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	lang, err := gotreesitter.LoadLanguage(blob)
	if err != nil {
		return false, "", fmt.Errorf("load %s_c: %w", name, err)
	}
	if len(lang.ExternalSymbols) == 0 {
		return true, "", nil
	}
	support, err := os.ReadFile("grammars/runtime/support_" + name + ".go")
	if err != nil {
		return false, "", fmt.Errorf("%s_c has external symbols and no scanner support: %w", name, err)
	}
	found := registeredScanner.FindSubmatch(support)
	if found == nil {
		return false, "", fmt.Errorf("%s_c has external symbols and support_%s.go registers no scanner", name, name)
	}
	return true, string(found[1]), nil
}

func wrapper(name string, blob []byte, support, ccore bool, scanner string) ([]byte, error) {
	accessor := accessorName(name)
	pkg := name
	if token.Lookup(pkg).IsKeyword() {
		pkg += "lang"
	}
	var register []string
	if support {
		register = append(register, "grammarruntime.Register"+accessor+"Support()")
	}
	register = append(register, fmt.Sprintf("grammarruntime.RegisterBlob(%q, grammarblobs.%s)", name, accessor))
	languageC := ""
	if ccore {
		register = append(register, fmt.Sprintf("gotreesitter.RegisterCCoreLanguage(%q, LanguageC)", name))
		attach := ""
		if scanner != "" {
			attach = "lang.ExternalScanner = grammarruntime." + scanner + "{}.ExternalScannerForLanguage(lang)\n"
		}
		languageC = "func LanguageC() *gotreesitter.Language {\nlang, err := gotreesitter.LoadLanguage(grammarblobs." + accessor + "C())\nif err != nil {\nreturn nil\n}\n" + attach + "return lang\n}\n\n"
	}
	once := "registered.Do(func() { " + register[0] + " })"
	if len(register) > 1 {
		once = "registered.Do(func() {\n" + strings.Join(register, "\n") + "\n})"
	}
	return format.Source([]byte(fmt.Sprintf(`//go:build !grammar_subset || grammar_subset_%s

package %s

import (
"sync"

"github.com/odvcencio/gotreesitter"
grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
)

const BlobSHA256 = "%x"

var registered sync.Once

%sfunc Language() *gotreesitter.Language {
%s
return grammarruntime.Language(%q)
}
`, name, pkg, sha256.Sum256(blob), languageC, once, name+".bin")))
}

func accessorName(name string) string { return strings.ToUpper(name[:1]) + name[1:] }

func cAccessor(name string) []byte {
	accessor := accessorName(name) + "C"
	return []byte(fmt.Sprintf("package grammarblobs\n\nimport _ \"embed\"\n\n//go:embed %s_c.bin\nvar blob%s []byte\n\nfunc %s() []byte { return blob%s }\n", name, accessor, accessor, accessor))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

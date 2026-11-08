package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/odvcencio/gotreesitter"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
	"github.com/pierrec/lz4/v4"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: flatblob <source dir> <target dir>")
		os.Exit(2)
	}
	paths, err := filepath.Glob(filepath.Join(os.Args[1], "*.bin"))
	if err != nil || len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "flatblob: no grammar blobs in %s\n", os.Args[1])
		os.Exit(1)
	}
	for _, path := range paths {
		if err := convert(path, filepath.Join(os.Args[2], filepath.Base(path))); err != nil {
			fmt.Fprintf(os.Stderr, "flatblob: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func convert(path, target string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lang, err := decode(data)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".bin")
	grammarruntime.RepairDecodedLanguage(name, lang)
	if err := gotreesitter.ValidateLanguage(lang); err != nil {
		return err
	}
	blob := gotreesitter.EncodeLanguageBlob(lang, compress)
	back, err := gotreesitter.LoadLanguage(blob)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	if !sameExported(reflect.ValueOf(lang).Elem(), reflect.ValueOf(back).Elem()) {
		return fmt.Errorf("reload differs from the encoded language")
	}
	if !bytes.Equal(gotreesitter.EncodeLanguageBlob(back, compress), blob) {
		return fmt.Errorf("re-encoding the reload changes the bytes")
	}
	if err := os.WriteFile(target, blob, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s %x %x %d %d\n", name, sha256.Sum256(data), sha256.Sum256(blob), len(data), len(blob))
	return nil
}

func compress(raw []byte) []byte {
	out := make([]byte, lz4.CompressBlockBound(len(raw)))
	hc := lz4.CompressorHC{Level: lz4.Level9}
	n, err := hc.CompressBlock(raw, out)
	if err != nil || n == 0 {
		panic(fmt.Sprintf("lz4 compress: %d bytes, %v", n, err))
	}
	return out[:n]
}

func decode(data []byte) (*gotreesitter.Language, error) {
	if bytes.HasPrefix(data, []byte("GTSFLAT1")) {
		return gotreesitter.LoadLanguage(data)
	}
	if bytes.HasPrefix(data, []byte("GTSVER1\x00")) && len(data) >= 16 {
		data = data[16+int(binary.BigEndian.Uint16(data[14:16])):]
	}
	trailer := bytes.HasPrefix(data, []byte("GTSBLOB\x00"))
	if trailer {
		data = data[20:]
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	stream := bytes.NewReader(raw)
	lang := &gotreesitter.Language{}
	if err := gob.NewDecoder(stream).Decode(lang); err != nil {
		return nil, err
	}
	if trailer {
		var pairs []struct {
			Key    uint64
			Target gotreesitter.StateID
		}
		if err := gob.NewDecoder(stream).Decode(&pairs); err != nil {
			return nil, err
		}
		lang.LargeStateGotos = map[uint64]gotreesitter.StateID{}
		for _, p := range pairs {
			lang.LargeStateGotos[p.Key] = p.Target
		}
	}
	return lang, nil
}

func sameExported(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Slice:
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !sameExported(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if a.Type().Field(i).IsExported() && !sameExported(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		for _, key := range a.MapKeys() {
			if other := b.MapIndex(key); !other.IsValid() || !sameExported(a.MapIndex(key), other) {
				return false
			}
		}
		return true
	case reflect.Interface, reflect.Pointer, reflect.Func:
		return a.IsNil() && b.IsNil()
	default:
		return a.Interface() == b.Interface()
	}
}

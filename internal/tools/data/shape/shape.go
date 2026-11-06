package shape

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const Name = "shape"

type kind uint8

const (
	kindNull kind = 1 << iota
	kindBool
	kindInt
	kindFloat
	kindString
	kindObject
	kindArray
)

type shape struct {
	kinds   kind
	values  int
	objects int
	fields  []*field
	index   map[string]*field
	items   *shape
}

type field struct {
	key          string
	shape        shape
	present      int
	seenAt       int
	firstMissing int
	nulls        int
	firstNull    int
}

type Limits struct {
	Bytes, Context int
}

type Shape struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Shape{root: root, limits: limits}, err
}

func (Shape) Name() string { return Name }

func (Shape) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "prints the type of a JSON sample in the project's language: a Go struct, a TypeScript interface, a Rust serde struct or a Python TypedDict, picked from go.mod, Cargo.toml, a python project file, tsconfig.json or package.json. " +
			"use it instead of reading a sample and guessing a type from its first item: every item is merged, so a field that is null or missing in item 40 of 50 is marked optional and a field holding a string in one item and a number in another is marked a union, and each is listed under marked with the item it first happens in. " +
			"give the JSON as sample, or a file as path; several JSON values one after another (JSON lines) are read as the items of one array. invalid JSON is refused with its line, column and byte",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sample":   map[string]any{"type": "string", "description": "the JSON text itself"},
				"path":     map[string]any{"type": "string", "description": "a file holding the JSON, instead of sample"},
				"name":     map[string]any{"type": "string", "description": "the name of the top type, Root when left out"},
				"language": map[string]any{"type": "string", "enum": []string{golang.String(), typescript.String(), rust.String(), python.String()}, "description": "only needed when the project is not the language wanted"},
			},
		},
	}
}

func (s Shape) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Sample   string `json:"sample"`
		Path     string `json:"path"`
		Name     string `json:"name"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("shape: arguments are not the expected shape: %w", err)
	}
	text, from, refusal := s.input(args.Sample, args.Path)
	if refusal != "" {
		return refused(refusal), nil
	}
	lang, picked, refusal := s.language(args.Language)
	if refusal != "" {
		return refused(refusal), nil
	}
	if err := valid(text); err != nil {
		return refused(from + " is not JSON: " + s.position(text, err)), nil
	}
	var top shape
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	for dec.More() {
		if _, err := top.read(dec); err != nil {
			return contract.Result{}, fmt.Errorf("shape: reading JSON already checked as valid: %w", err)
		}
	}
	subject, holds := &top, strconv.Itoa(top.values)+" JSON values one after another"
	switch {
	case top.values == 0:
		return refused(from + " holds no JSON value"), nil
	case top.values == 1 && top.kinds == kindArray:
		subject, holds = top.items, "an array of "+strconv.Itoa(top.items.values)
	case top.values == 1:
		holds = "one value"
	}
	name := "Root"
	if args.Name != "" {
		name = pascal(args.Name)
	}
	r := renderer{lang: lang, names: map[string]bool{}}
	typed := r.typeOf(subject, name)
	return contract.Result{Content: "shape: " + lang.String() + " " + picked + "; " + from + " holds " + holds + ", typed " + typed + "\n\n" + r.render(), Command: Name}, nil
}

func refused(said string) contract.Result {
	return contract.Failure("shape: " + said + "\n")
}

func (s Shape) input(sample, path string) (string, string, string) {
	if (sample == "") == (path == "") {
		return "", "", "give the JSON as sample or a file as path, one of the two"
	}
	if path == "" {
		if len(sample) > s.limits.Bytes {
			return "", "", "the sample is " + strconv.Itoa(len(sample)) + " bytes, over the limit of " + strconv.Itoa(s.limits.Bytes) + ": cut it to a part"
		}
		return sample, "the sample", ""
	}
	resolved, err := s.root.Resolve(path)
	if err != nil {
		return "", "", err.Error()
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err.Error()
	}
	if info.Size() > int64(s.limits.Bytes) {
		return "", "", path + " is " + strconv.FormatInt(info.Size(), 10) + " bytes, over the limit of " + strconv.Itoa(s.limits.Bytes) + ": cut it to a part"
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", err.Error()
	}
	return strings.TrimPrefix(string(body), "\xef\xbb\xbf"), path, ""
}

func (s Shape) language(asked string) (language, string, string) {
	if asked != "" {
		for _, l := range []language{golang, typescript, rust, python} {
			if l.String() == asked {
				return l, "as asked", ""
			}
		}
		return 0, "", "language must be go, ts, rust or python, not " + strconv.Quote(asked)
	}
	markers := []struct {
		language language
		file     string
	}{
		{golang, "go.mod"}, {rust, "Cargo.toml"},
		{python, "pyproject.toml"}, {python, "setup.py"}, {python, "setup.cfg"}, {python, "requirements.txt"},
		{typescript, "tsconfig.json"}, {typescript, "package.json"},
	}
	for at := string(s.root); ; at = filepath.Dir(at) {
		for _, marker := range markers {
			if found := filepath.Join(at, marker.file); contract.IsFile(found) {
				return marker.language, "from " + found, ""
			}
		}
		if filepath.Dir(at) == at {
			return 0, "", "no go.mod, Cargo.toml, python project file, tsconfig.json or package.json in " + string(s.root) + " or above it: name the language"
		}
	}
}

func valid(text string) error {
	dec := json.NewDecoder(strings.NewReader(text))
	for {
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (s Shape) position(text string, err error) string {
	at := len(text)
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		at = min(max(int(syntax.Offset)-1, 0), len(text))
	}
	lineStart := strings.LastIndexByte(text[:at], '\n') + 1
	lineEnd := len(text)
	if end := strings.IndexByte(text[at:], '\n'); end >= 0 {
		lineEnd = at + end
	}
	from, to := max(lineStart, at-s.limits.Context), min(lineEnd, at+s.limits.Context)
	for from > lineStart && !utf8.RuneStart(text[from]) {
		from--
	}
	for to < lineEnd && !utf8.RuneStart(text[to]) {
		to++
	}
	near := strings.NewReplacer("\t", " ", "\r", " ").Replace(text[from:to])
	return "line " + strconv.Itoa(strings.Count(text[:at], "\n")+1) + ", column " + strconv.Itoa(utf8.RuneCountInString(text[lineStart:at])+1) + ", byte " + strconv.Itoa(at) + ": " + strings.TrimPrefix(err.Error(), "json: ") +
		"\n  " + near + "\n  " + strings.Repeat(" ", utf8.RuneCountInString(text[from:at])) + "^"
}

func (s *shape) read(dec *json.Decoder) (kind, error) {
	token, err := dec.Token()
	if err != nil {
		return 0, err
	}
	s.values++
	var got kind
	switch t := token.(type) {
	case nil:
		got = kindNull
	case bool:
		got = kindBool
	case string:
		got = kindString
	case json.Number:
		got = kindFloat
		if !strings.ContainsAny(string(t), ".eE") {
			got = kindInt
		}
	case json.Delim:
		if t == '{' {
			got, err = kindObject, s.readObject(dec)
		} else {
			got, err = kindArray, s.readArray(dec)
		}
		if err == nil {
			_, err = dec.Token()
		}
	}
	s.kinds |= got
	return got, err
}

func (s *shape) readArray(dec *json.Decoder) error {
	if s.items == nil {
		s.items = &shape{}
	}
	for dec.More() {
		if _, err := s.items.read(dec); err != nil {
			return err
		}
	}
	return nil
}

func (s *shape) readObject(dec *json.Decoder) error {
	s.objects++
	if s.index == nil {
		s.index = map[string]*field{}
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := token.(string)
		f, known := s.index[key]
		if !known {
			f = &field{key: key}
			s.index[key] = f
			s.fields = append(s.fields, f)
		}
		if f.firstMissing == 0 && f.seenAt != s.objects-1 {
			f.firstMissing = f.seenAt + 1
		}
		f.present++
		f.seenAt = s.objects
		got, err := f.shape.read(dec)
		if err != nil {
			return err
		}
		if got == kindNull {
			f.nulls++
			f.firstNull = cmp.Or(f.firstNull, s.objects)
		}
	}
	return nil
}

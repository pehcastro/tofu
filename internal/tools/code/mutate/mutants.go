package mutate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type mutant struct {
	line       int
	start, end int
	to         string
}

func (m mutant) applied(body string) string { return body[:m.start] + m.to + body[m.end:] }

func (m mutant) change(body string) string {
	from := strings.LastIndexByte(body[:m.start], '\n') + 1
	to := len(body)
	if end := strings.IndexByte(body[m.end:], '\n'); end >= 0 {
		to = m.end + end
	}
	return strconv.Quote(strings.TrimSpace(body[from:to])) + " became " + strconv.Quote(strings.TrimSpace(body[from:m.start]+m.to+body[m.end:to]))
}

func flipped(op string) string {
	switch op {
	case "==":
		return "!="
	case "!=":
		return "=="
	case "===":
		return "!=="
	case "!==":
		return "==="
	case "<":
		return ">="
	case ">":
		return "<="
	case "<=":
		return ">"
	case ">=":
		return "<"
	case "&&":
		return "||"
	case "||":
		return "&&"
	case "and":
		return "or"
	case "or":
		return "and"
	case "+":
		return "-"
	case "-":
		return "+"
	case "*":
		return "/"
	case "/":
		return "*"
	case "true":
		return "false"
	case "false":
		return "true"
	case "True":
		return "False"
	case "False":
		return "True"
	}
	return ""
}

func located(body string, found []mutant) []mutant {
	slices.SortFunc(found, func(a, b mutant) int { return a.start - b.start })
	for i := range found {
		found[i].line = strings.Count(body[:found[i].start], "\n") + 1
	}
	return found
}

func goMutants(body string) ([]mutant, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "", body, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []mutant
	offset := func(at token.Pos) int { return files.Position(at).Offset }
	add := func(from, to token.Pos, replacement string) {
		found = append(found, mutant{start: offset(from), end: offset(to), to: replacement})
	}
	var visit func(results *ast.FieldList) func(ast.Node) bool
	visit = func(results *ast.FieldList) func(ast.Node) bool {
		return func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.FuncLit:
				ast.Inspect(n.Body, visit(n.Type.Results))
				return false
			case *ast.BinaryExpr:
				if to := flipped(n.Op.String()); to != "" && (n.Op != token.ADD || !isString(n.X) && !isString(n.Y)) {
					add(n.OpPos, n.OpPos+token.Pos(len(n.Op.String())), to)
				}
			case *ast.BasicLit:
				if value, err := strconv.ParseInt(n.Value, 0, 64); n.Kind == token.INT && err == nil {
					add(n.Pos(), n.End(), strconv.FormatInt(value+1, 10))
				}
			case *ast.Ident:
				if n.Name == "true" || n.Name == "false" {
					add(n.Pos(), n.End(), flipped(n.Name))
				}
			case *ast.ReturnStmt:
				if zero, known := goZeroes(results); known && len(n.Results) > 0 {
					from, to := n.Results[0].Pos(), n.Results[len(n.Results)-1].End()
					if body[offset(from):offset(to)] != zero {
						add(from, to, zero)
					}
				}
			}
			return true
		}
	}
	for _, decl := range file.Decls {
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Body != nil {
			ast.Inspect(fn.Body, visit(fn.Type.Results))
		}
	}
	return located(body, found), nil
}

func isString(expr ast.Expr) bool {
	literal, isLiteral := expr.(*ast.BasicLit)
	return isLiteral && literal.Kind == token.STRING
}

func goZeroes(results *ast.FieldList) (string, bool) {
	if results == nil {
		return "", false
	}
	var zeroes []string
	for _, field := range results.List {
		zero, known := goZero(field.Type)
		if !known {
			return "", false
		}
		for range max(1, len(field.Names)) {
			zeroes = append(zeroes, zero)
		}
	}
	return strings.Join(zeroes, ", "), true
}

func goZero(typ ast.Expr) (string, bool) {
	switch t := typ.(type) {
	case *ast.Ident:
		switch {
		case t.Name == "string":
			return `""`, true
		case t.Name == "bool":
			return "false", true
		case t.Name == "error" || t.Name == "any":
			return "nil", true
		case slices.Contains([]string{"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64"}, t.Name):
			return "0", true
		}
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return "nil", true
	case *ast.ArrayType:
		return "nil", t.Len == nil
	}
	return "", false
}

type syntax struct {
	lineComment  string
	blockComment bool
	quotes       string
	operators    *regexp.Regexp
	booleans     *regexp.Regexp
	zero         string
	testsFrom    string
}

func textMutants(body string, s syntax) []mutant {
	code := masked(body, s)
	if at := strings.Index(code, s.testsFrom); s.testsFrom != "" && at >= 0 {
		code = code[:at]
	}
	var found []mutant
	for _, at := range s.operators.FindAllStringSubmatchIndex(code, -1) {
		found = append(found, mutant{start: at[2], end: at[3], to: flipped(code[at[2]:at[3]])})
	}
	for _, at := range s.booleans.FindAllStringIndex(code, -1) {
		found = append(found, mutant{start: at[0], end: at[1], to: flipped(code[at[0]:at[1]])})
	}
	for _, at := range regexp.MustCompile(`\b[0-9][0-9A-Za-z_.]*`).FindAllStringIndex(code, -1) {
		if value, err := strconv.Atoi(code[at[0]:at[1]]); err == nil && (at[0] == 0 || code[at[0]-1] != '.') {
			found = append(found, mutant{start: at[0], end: at[1], to: strconv.Itoa(value + 1)})
		}
	}
	for _, at := range regexp.MustCompile(`(?m)\breturn[ \t]+([^;\r\n]*[^;\s])`).FindAllStringSubmatchIndex(code, -1) {
		if expr := code[at[2]:at[3]]; expr != s.zero && balanced(expr) {
			found = append(found, mutant{start: at[2], end: at[3], to: s.zero})
		}
	}
	return located(body, found)
}

func balanced(expr string) bool {
	depth := 0
	for _, char := range expr {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	return depth == 0
}

func masked(body string, s syntax) string {
	out := []byte(body)
	blank := func(from, length int) int {
		for i := from; i < from+length; i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
		return from + length
	}
	for i := 0; i < len(body); {
		rest := body[i:]
		switch {
		case strings.HasPrefix(rest, s.lineComment):
			i = blank(i, lengthUntil(rest, "\n", 0))
		case s.blockComment && strings.HasPrefix(rest, "/*"):
			i = blank(i, lengthUntil(rest, "*/", len("*/")))
		case strings.IndexByte(s.quotes, rest[0]) >= 0:
			i = blank(i, quotedLength(rest))
		default:
			i++
		}
	}
	return string(out)
}

func lengthUntil(rest, end string, kept int) int {
	if at := strings.Index(rest[1:], end); at >= 0 {
		return 1 + at + kept
	}
	return len(rest)
}

func quotedLength(rest string) int {
	quote := rest[:1]
	if strings.HasPrefix(rest, strings.Repeat(quote, 3)) {
		quote = rest[:3]
	}
	for i := len(quote); i < len(rest); i++ {
		switch {
		case rest[i] == '\\':
			i++
		case strings.HasPrefix(rest[i:], quote):
			return i + len(quote)
		}
	}
	return len(rest)
}

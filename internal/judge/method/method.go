package method

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

type Method string

const (
	Cheap   Method = "cheap"
	Judged  Method = "judged"
	Unwired Method = "unwired"
)

func (m Method) String() string {
	switch m {
	case Cheap:
		return "the cheap method"
	case Judged:
		return "the judged method"
	case Unwired:
		return "no method"
	}
	panic("method: unknown method " + string(m))
}

type Choice struct {
	Point    string
	Method   Method
	Why      string
	Measured string
	Cost     string
	Line     int
}

type Table struct {
	Version int
	Notes   string
	File    string
	Choices []Choice
}

const TableFile = "decisions/methods@1.yaml"

const TableKind = "method_table"

func Load(shipped fs.FS) (Table, error) {
	data, err := fs.ReadFile(shipped, TableFile)
	if err != nil {
		return Table{}, err
	}
	return Parse(data, "library/"+TableFile)
}

type UnknownPointError struct {
	Point string
	File  string
}

func (e UnknownPointError) Error() string {
	return fmt.Sprintf("%s names no method for %s, and a point decides nothing until it is named there", e.File, e.Point)
}

type UnwiredError struct {
	Choice Choice
	File   string
}

func (e UnwiredError) Error() string {
	return fmt.Sprintf("%s:%d leaves %s unwired: %s", e.File, e.Choice.Line, e.Choice.Point, e.Choice.Why)
}

type MissingArmError struct {
	Point  string
	Method Method
}

func (e MissingArmError) Error() string {
	return fmt.Sprintf("%s is decided by %s and the caller passed no such arm", e.Point, e.Method)
}

func (t Table) Of(point string) (Choice, error) {
	for _, c := range t.Choices {
		if c.Point == point {
			return c, nil
		}
	}
	return Choice{}, UnknownPointError{Point: point, File: t.File}
}

type Arms[T any] struct {
	Cheap  func(context.Context) (T, error)
	Judged func(context.Context) (T, error)
}

func Run[T any](ctx context.Context, table Table, point string, arms Arms[T]) (T, Method, error) {
	var zero T
	chosen, err := table.Of(point)
	if err != nil {
		return zero, Unwired, err
	}
	switch chosen.Method {
	case Judged:
		if arms.Judged == nil {
			return zero, Judged, MissingArmError{Point: point, Method: Judged}
		}
		value, err := arms.Judged(ctx)
		return value, Judged, err
	case Cheap:
		if arms.Cheap == nil {
			return zero, Cheap, MissingArmError{Point: point, Method: Cheap}
		}
		value, err := arms.Cheap(ctx)
		return value, Cheap, err
	case Unwired:
		return zero, Unwired, UnwiredError{Choice: chosen, File: table.File}
	}
	panic("method: unknown method " + string(chosen.Method))
}

func Parse(data []byte, path string) (Table, error) {
	table := Table{File: path}
	inMethods := false
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := i + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if strings.ContainsRune(raw, '\t') {
			return Table{}, fmt.Errorf("%s:%d: a tab in the indent", path, line)
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		key, value, found := strings.Cut(strings.TrimSpace(raw), ":")
		if !found || key == "" {
			return Table{}, fmt.Errorf("%s:%d: expected key: value, found %q", path, line, strings.TrimSpace(raw))
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch indent {
		case 0:
			inMethods = key == "methods"
			if inMethods {
				continue
			}
			if err := table.setField(key, value, path, line); err != nil {
				return Table{}, err
			}
		case 2:
			if !inMethods {
				return Table{}, fmt.Errorf("%s:%d: %q sits outside the methods block", path, line, key)
			}
			if _, err := table.Of(key); err == nil {
				return Table{}, fmt.Errorf("%s:%d: %s is named twice and a point has one method", path, line, key)
			}
			table.Choices = append(table.Choices, Choice{Point: key, Line: line})
		case 4:
			if len(table.Choices) == 0 {
				return Table{}, fmt.Errorf("%s:%d: %q belongs to no point", path, line, key)
			}
			if err := table.Choices[len(table.Choices)-1].setField(key, value, path, line); err != nil {
				return Table{}, err
			}
		default:
			return Table{}, fmt.Errorf("%s:%d: an indent of %d, and this file indents by two", path, line, indent)
		}
	}
	return table, table.validate()
}

func (t *Table) setField(key, value, path string, line int) error {
	switch key {
	case "kind":
		if value != TableKind {
			return fmt.Errorf("%s:%d: kind is %q, found %q", path, line, TableKind, value)
		}
	case "table_version":
		version, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: table_version is a whole number, found %q", path, line, value)
		}
		t.Version = version
	case "notes":
		t.Notes = value
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func (c *Choice) setField(key, value, path string, line int) error {
	switch key {
	case "method":
		m := Method(value)
		if m != Cheap && m != Judged && m != Unwired {
			return fmt.Errorf("%s:%d: method is %s, %s or %s, found %q", path, line, string(Cheap), string(Judged), string(Unwired), value)
		}
		c.Method = m
	case "why":
		c.Why = value
	case "measured":
		c.Measured = value
	case "cost":
		c.Cost = value
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func (t Table) validate() error {
	if t.Version < 1 {
		return fmt.Errorf("%s: the table declares no table_version", t.File)
	}
	if len(t.Choices) == 0 {
		return fmt.Errorf("%s: the table names no decision point", t.File)
	}
	for _, c := range t.Choices {
		if c.Method == "" {
			return fmt.Errorf("%s:%d: %s names no method, and unwired is said rather than left out", t.File, c.Line, c.Point)
		}
		if c.Why == "" {
			return fmt.Errorf("%s:%d: %s names no why, and a method with no reason cannot be argued with", t.File, c.Line, c.Point)
		}
		if c.Method != Unwired && c.Cost == "" {
			return fmt.Errorf("%s:%d: %s is wired and states no cost", t.File, c.Line, c.Point)
		}
	}
	return nil
}

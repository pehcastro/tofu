package search

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	tsMethod = `import { Item } from "./item";

const label = "a { brace in a string";

export class Cart {
  items: Item[] = [];

  total(): number {
    const parts = ~${this.items.length} items {~;
    return this.items.reduce((sum, item) => sum + needle(item), 0);
  }
}
`
	tsArrow = `export const describe = async (
  cart: Cart,
): Promise<string> => {
  const re = /[{]+/g;
  const nested = ~a ${~b ${cart.id}~} {~;
  return needle(cart, re, nested);
};
`
	tsNested = `const x = 1
function outer({ a, b }: Opts) {
  function inner() {
    return needle(a)
  }
  return inner() + b
}
`
	tsAliasBefore = `type State = Omit<Interface, "generate">

export class Service extends Base<Service>()("needle") {}
`
	tsCall = `export const handler = define(async (req) => {
  return needle(req)
})
`
	tsDecorated = `@Component({
  selector: "app",
})
export class Widget {
  render() {
    if (this.ready) {
      %% needle in a comment {
    }
  }
}
`
	tsxApostrophe = `export function Hello() {
  return (
    <p>
      Don't {needle} me
    </p>
  );
}
`
	tsBroken = `function broken() {
  return needle(

`
	tsOutside = `import { needle } from "x";
function f() {
  return 1;
}
`
	tsValue = `export const config = {
  needle: 1,
};
`
	pyMethod = `import functools


class Cart:
    def __init__(self):
        self.items = []

    @functools.lru_cache(
        maxsize=None,
    )
    def total(self):
        note = "a { brace and a : colon"
        return sum(needle(item) for item in self.items)

    # trailing comment


def describe(cart):
    return f"cart of {len(cart.items)}"
`
	pyNested = `def outer(
    a,
    b,
):
    """
def fake():
"""
    def inner():
        return needle(a) + \
b
    return inner()


x = 1
`
	pyCRLF   = "def f():\r\n    return needle()\r\n\r\nx = 1\r\n"
	pyBroken = "def f():\n    s = \"\"\"\n    needle\n"
	pyString = "def g():\n    return \"needle\"\n"
	rsImpl   = `use std::fmt;

#[derive(Debug)]
pub struct Cart {
    items: Vec<u32>,
}

impl<T> fmt::Display for Cart<T> {
    fn fmt<'a>(&'a self, f: &mut fmt::Formatter) -> fmt::Result {
        let brace = '{';
        let note = "a } brace";
        <* nested <* } *> brace *>
        needle(f)
    }
}
`
	rsModule = `mod tests {
    use super::*;

    #[test]
    fn works() {
        let raw = r#"a { "quoted" brace"#;
        assert!(needle());
    }
}
`
	rsBroken = "fn f() {\n    needle();\n"
)

func TestAMatchOutsideGoComesBackAsTheUnitAroundIt(t *testing.T) {
	commentMarkers := strings.NewReplacer("~", "`", "%%", "/"+"/", "<*", "/"+"*", "*>", "*"+"/")
	for _, row := range []struct {
		file, source, pattern string
		budget                int
		want                  string
	}{
		{file: "a.ts", source: tsMethod, pattern: "needle", want: "method Cart.total 8-11 fallback=false code x1"},
		{file: "a.ts", source: tsArrow, pattern: "needle", want: "func describe 1-7 fallback=false code x1"},
		{file: "a.ts", source: tsNested, pattern: "needle", want: "func outer 2-7 fallback=false code x1"},
		{file: "a.ts", source: tsAliasBefore, pattern: "needle", want: "type Service 3-3 fallback=false string x1"},
		{file: "a.ts", source: tsCall, pattern: "needle", want: "value handler 1-3 fallback=false code x1"},
		{file: "a.ts", source: tsDecorated, pattern: "needle", want: "method Widget.render 5-9 fallback=false comment x1"},
		{file: "a.ts", source: tsDecorated, pattern: "selector", want: "type Widget 1-10 fallback=false code x1"},
		{file: "a.tsx", source: tsxApostrophe, pattern: "needle", want: "func Hello 1-7 fallback=false code x1"},
		{file: "a.js", source: tsBroken, pattern: "needle", want: "lines  1-3 fallback=true unparsed x1"},
		{file: "a.ts", source: tsOutside, pattern: "needle", want: "lines  1-4 fallback=false code x1"},
		{file: "a.ts", source: tsValue, pattern: "needle", want: "value config 1-3 fallback=false code x1"},
		{file: "a.py", source: pyMethod, pattern: "needle", want: "method Cart.total 8-13 fallback=false code x1"},
		{file: "a.py", source: pyMethod, pattern: "needle", budget: 100, want: ""},
		{file: "a.py", source: pyNested, pattern: "needle", want: "func outer 1-11 fallback=false code x1"},
		{file: "a.py", source: pyNested, pattern: "fake", want: "func outer 1-11 fallback=false string x1"},
		{file: "a.py", source: pyCRLF, pattern: "needle", want: "func f 1-2 fallback=false code x1"},
		{file: "a.py", source: pyBroken, pattern: "needle", want: "lines  1-3 fallback=true unparsed x1"},
		{file: "a.py", source: pyString, pattern: "needle", want: "func g 1-2 fallback=false string x1"},
		{file: "a.rs", source: rsImpl, pattern: "needle", want: "method Cart.fmt 9-14 fallback=false code x1"},
		{file: "a.rs", source: rsImpl, pattern: "needle|brace", want: "method Cart.fmt 9-14 fallback=false code x4"},
		{file: "a.rs", source: rsImpl, pattern: "items", want: "type Cart 3-6 fallback=false code x1"},
		{file: "a.rs", source: rsModule, pattern: "needle", want: "func works 4-8 fallback=false code x1"},
		{file: "a.rs", source: rsModule, pattern: "super", want: "lines  1-5 fallback=false code x1"},
		{file: "a.rs", source: rsBroken, pattern: "needle", want: "lines  1-2 fallback=true unparsed x1"},
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, row.file), []byte(commentMarkers.Replace(row.source)), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := Find(Request{Root: root, Files: []string{row.file}, Pattern: regexp.MustCompile(row.pattern), MaxTokens: row.budget})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if len(result.Units) > 0 {
			unit := result.Units[0]
			got = fmt.Sprintf("%s %s %d-%d fallback=%v %s x%d", unit.Kind, unit.Symbol, unit.FirstLine, unit.LastLine, unit.Fallback, unit.Matches[0].Placement, len(unit.Matches))
		}
		if got != row.want || (row.want == "" && result.Stats.Units != 1) {
			t.Errorf("%s /%s/ in:\n%s\ngot  %q\nwant %q\n%s", row.file, row.pattern, row.source, got, row.want, result.Text)
		}
	}
}

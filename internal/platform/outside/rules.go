//go:build ruleguard

package outside

import "github.com/quasilyte/go-ruleguard/dsl"

func clientBuiltByHand(m dsl.Matcher) {
	m.Import("net/http")
	m.Match(`$x{$*_}`, `new($x)`, `var $_ $x`, `var $_ $x = $_`).
		Where(m["x"].Type.Is(`http.Client`) &&
			!m.File().Name.Matches(`_test\.go$`) &&
			!m.File().PkgPath.Matches(`^tofu/(bench|internal/platform/outside)(/|$)`)).
		Report(`build the client with outside.Client or outside.Over so the wall clock counts the wait`)
}

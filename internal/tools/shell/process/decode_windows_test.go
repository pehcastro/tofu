//go:build windows

package shell

import "testing"

func TestDecodeReadsEachLineInTheCodePageItWasWrittenIn(t *testing.T) {
	const oem850, legacy437 = 850, 437
	chcpLine := "P\xa0gina de c\xa2digo ativa: 65001\r\n"
	chcpRead := "Página de código ativa: 65001\r\n"
	for _, row := range []struct {
		name         string
		output       string
		console, oem uint32
		want         string
	}{
		{"a killed bun left the console at 65001", chcpLine, codePageUTF8, oem850, chcpRead},
		{"no console", chcpLine, 0, oem850, chcpRead},
		{"a legacy console page", chcpLine, legacy437, oem850, chcpRead},
		{"utf-8 under a legacy page", "café\n", oem850, oem850, "café\n"},
		{"one utf-8 line and one oem line", "café\n" + chcpLine, codePageUTF8, oem850, "café\n" + chcpRead},
		{"the oem page is utf-8 too", chcpLine, codePageUTF8, codePageUTF8, chcpLine},
		{"a trailing newline", "caf\x82\n", 0, oem850, "café\n"},
	} {
		if got := decodeIn([]byte(row.output), row.console, row.oem); got != row.want {
			t.Errorf("%s: %q, want %q", row.name, got, row.want)
		}
	}
}

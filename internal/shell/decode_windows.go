//go:build windows

package shell

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

const codePageUTF8 = 65001

func Decode(output []byte) string {
	if utf8.Valid(output) {
		return string(output)
	}
	console, _ := windows.GetConsoleOutputCP()
	oem, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP").Call()
	return decodeIn(output, console, uint32(oem))
}

func decodeIn(output []byte, console, oem uint32) string {
	page := console
	if page == 0 || page == codePageUTF8 {
		page = oem
	}
	if page == codePageUTF8 {
		return string(output)
	}
	var decoded strings.Builder
	for _, line := range bytes.SplitAfter(output, []byte("\n")) {
		if utf8.Valid(line) {
			decoded.Write(line)
			continue
		}
		wide := make([]uint16, len(line))
		n, err := windows.MultiByteToWideChar(page, 0, &line[0], int32(len(line)), &wide[0], int32(len(wide)))
		if err != nil {
			decoded.Write(line)
			continue
		}
		decoded.WriteString(string(utf16.Decode(wide[:n])))
	}
	return decoded.String()
}

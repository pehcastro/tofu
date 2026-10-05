//go:build windows

package sys

import "golang.org/x/sys/windows"

type ConsolePages struct{ input, output uint32 }

func ReadConsolePages() ConsolePages {
	input, _ := windows.GetConsoleCP()
	output, _ := windows.GetConsoleOutputCP()
	return ConsolePages{input: input, output: output}
}

func (found ConsolePages) Restore() {
	now := ReadConsolePages()
	if found.input != 0 && found.input != now.input {
		_ = windows.SetConsoleCP(found.input)
	}
	if found.output != 0 && found.output != now.output {
		_ = windows.SetConsoleOutputCP(found.output)
	}
}

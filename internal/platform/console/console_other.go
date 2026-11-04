//go:build !windows

package console

type ConsolePages struct{}

func ReadConsolePages() ConsolePages { return ConsolePages{} }

func (ConsolePages) Restore() {}

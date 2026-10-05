//go:build !windows

package sys

type ConsolePages struct{}

func ReadConsolePages() ConsolePages { return ConsolePages{} }

func (ConsolePages) Restore() {}

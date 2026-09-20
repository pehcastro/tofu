package main

import (
	"fmt"
	"io"

	"boji/interface/tui/frame"
	"boji/internal/sys"
)

func version(out io.Writer) int {
	_, _ = fmt.Fprintf(out, "version: %s\n", frame.Release(sys.Version(), sys.BuildRevision()))
	_, _ = fmt.Fprintf(out, "commit: %s\n", sys.BuildRevision())
	_, _ = fmt.Fprintf(out, "go: %s\n", sys.GoVersion())
	return exitOK
}

package main

import (
	"fmt"
	"io"

	"boji/internal/sys"
)

func version(out io.Writer) int {
	_, _ = fmt.Fprintf(out, "version: %s\n", sys.Version())
	_, _ = fmt.Fprintf(out, "commit: %s\n", sys.BuildRevision())
	_, _ = fmt.Fprintf(out, "go: %s\n", sys.GoVersion())
	return exitOK
}

package tools

import "time"

type Outcome struct {
	Version string
	Hit     bool
	Bytes   int
	Calls   int
	Cost    float64
	Elapsed time.Duration
	Detail  string
	Skipped string
}

//go:build !windows && !darwin && !linux

package main

import "time"

func processStarted() (time.Time, bool) { return time.Time{}, false }

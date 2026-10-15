//go:build !windows

package main

import (
	"errors"
	"io"
	"time"
)

type console struct{}

func launch([]string, string, []string) (*console, error) {
	return nil, errors.New("longsession measures a Windows pseudo console; on another system drive tofu under script(1) and read /proc instead")
}

func (*console) drain(io.Writer)             {}
func (*console) Write(p []byte) (int, error) { return len(p), nil }
func (*console) read() (reading, error)      { return reading{}, errors.New("no console") }
func (*console) quit(time.Duration) bool     { return false }
func (*console) close()                      {}

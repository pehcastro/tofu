//go:build !windows

package main

import (
	"errors"
	"time"
)

type console struct{}

var errNoPseudoConsole = errors.New("the startup bench drives a Windows pseudo console, and no pty driver is written for this OS yet")

func launch([]string, string, []string, screenSize) (*console, time.Time, error) {
	return nil, time.Time{}, errNoPseudoConsole
}

func (*console) Read([]byte) (int, error) { return 0, errNoPseudoConsole }

func (*console) Write([]byte) (int, error) { return 0, errNoPseudoConsole }

func (*console) quit() {}

func (*console) close() {}

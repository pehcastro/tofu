//go:build !windows

package main

import (
	"errors"
	"image"
	"time"
)

type deskWindow struct{}

func deskKeys() map[string]uintptr { return nil }

func openDesk(string, []string, []string, string) (*deskWindow, error) {
	return nil, errors.New("tofu drive desk drives the desk on Windows only so far")
}

func (*deskWindow) click(image.Point) error       { return nil }
func (*deskWindow) typeText(string) error         { return nil }
func (*deskWindow) press(uintptr) error           { return nil }
func (*deskWindow) capture() (*image.RGBA, error) { return nil, nil }
func (*deskWindow) close(time.Duration) string    { return "" }

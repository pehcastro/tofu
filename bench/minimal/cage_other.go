//go:build !linux && !windows

package main

import (
	"errors"
	"os/exec"
)

type cage struct{}

const mechanism = "none"

func confine(*exec.Cmd, limits) (cage, error) {
	return cage{}, errors.New("bench/minimal limits a tree with a Linux cgroup or a Windows job object, and this system has neither")
}

func (cage) sample() reading { return reading{} }

func (cage) finish() finished { return finished{} }

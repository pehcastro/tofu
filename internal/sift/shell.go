package sift

import (
	"fmt"
	"strings"
)

type Shell struct {
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
}

type Stream string

const (
	StandardOutput Stream = "standard output"
	StandardError  Stream = "standard error"
)

type Held string

const (
	NotHeld       Held = ""
	HeldStderr    Held = "standard error"
	HeldExit      Held = "the exit status"
	HeldFirstUnit Held = "the first unit"
	HeldLastUnit  Held = "the last unit"
	HeldCode      Held = "a code block"
)

type Unit struct {
	Index  int
	Text   string
	Stream Stream
	Held   Held
	Lines  int
}

const shellUnitLineCap = 25

func SplitShell(result Shell) []Unit {
	units := cut(result.Stdout, StandardOutput)
	if len(units) > 0 {
		units[0].Held = HeldFirstUnit
		units[len(units)-1].Held = HeldLastUnit
	}
	for _, unit := range cut(result.Stderr, StandardError) {
		unit.Held = HeldStderr
		units = append(units, unit)
	}
	if result.ExitCode != 0 {
		units = append(units, Unit{Text: fmt.Sprintf("the command exited %d\n", result.ExitCode), Stream: StandardError, Held: HeldExit, Lines: 1})
	}
	for i := range units {
		units[i].Index = i
	}
	return units
}

func cut(text string, stream Stream) []Unit {
	var units []Unit
	var body strings.Builder
	content := 0
	closeUnit := func() {
		if body.Len() == 0 {
			return
		}
		units = append(units, Unit{Text: body.String(), Stream: stream, Lines: content})
		body.Reset()
		content = 0
	}
	trailing := false
	for _, line := range lines(text) {
		if blank(line) {
			if body.Len() == 0 && len(units) > 0 {
				units[len(units)-1].Text += line
			} else {
				body.WriteString(line)
			}
			trailing = true
			continue
		}
		if separator(line) {
			closeUnit()
			units = append(units, Unit{Text: line, Stream: stream, Lines: 1})
			trailing = false
			continue
		}
		if (trailing && content > 0) || content >= shellUnitLineCap {
			closeUnit()
		}
		trailing = false
		body.WriteString(line)
		content++
	}
	closeUnit()
	return units
}

func separator(line string) bool {
	flat := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if len(flat) < 3 {
		return false
	}
	return strings.Trim(flat, "-=_*#") == ""
}

func JoinUnits(units []Unit) string {
	var out strings.Builder
	for _, unit := range units {
		out.WriteString(unit.Text)
	}
	return out.String()
}

type Mode string

const (
	ModeShadow   Mode = "shadow"
	ModeEnforced Mode = "enforced"
)

func Message(units []Unit, marks []Mark, mode Mode) string {
	if mode == ModeShadow {
		return JoinUnits(units)
	}
	var out strings.Builder
	kept, keptBytes, total := 0, 0, 0
	for i, unit := range units {
		total += len(unit.Text)
		if marks[i].Keep {
			kept++
			keptBytes += len(unit.Text)
			out.WriteString(unit.Text)
			continue
		}
		fmt.Fprintf(&out, "[sift: %d lines, %d bytes elided, %s]\n", unit.Lines, len(unit.Text), marks[i].Reason)
	}
	if kept == len(units) {
		return out.String()
	}
	fmt.Fprintf(&out, "[sift: kept %d of %d units, %d of %d bytes]\n", kept, len(units), keptBytes, total)
	return out.String()
}

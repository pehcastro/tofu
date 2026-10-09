package main

import (
	"io"
	"strings"
	"time"

	"tofu/interface/tui/progress"
	"tofu/internal/widget"
)

func resumeNote(carry sessionResume) string {
	note := "resumed " + carry.Handle + ", carrying " + countOf(carry.Carried, "message") + " · " + sessionSteps(carry.Steps)
	switch {
	case carry.Fresh != "":
		return "started a new session: " + carry.Fresh
	case carry.Session == "":
		return ""
	case carry.HeadDerived:
		return note + ". no head was written, so tofu took the newest"
	}
	return note
}

func restoring(out io.Writer, carry sessionResume) func() {
	if carry.Session == "" || !isTerminal(out) {
		return func() {}
	}
	label := "restoring " + carry.Handle
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(progress.TickInterval)
		defer tick.Stop()
		for frame := 0; ; frame++ {
			_, _ = io.WriteString(out, "\r"+progress.Spin(frame)+" "+label)
			select {
			case <-stop:
				_, _ = io.WriteString(out, "\r"+strings.Repeat(" ", widget.Cells(label)+2)+"\r")
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		close(stop)
		<-stopped
	}
}

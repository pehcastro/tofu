package tea

import (
	"time"
)

func Batch(cmds ...Cmd) Cmd {
	return compactCmds[BatchMsg](cmds)
}

type BatchMsg []Cmd

func Sequence(cmds ...Cmd) Cmd {
	return compactCmds[sequenceMsg](cmds)
}

type sequenceMsg []Cmd

func compactCmds[T ~[]Cmd](cmds []Cmd) Cmd {
	var validCmds []Cmd
	for _, c := range cmds {
		if c == nil {
			continue
		}
		validCmds = append(validCmds, c)
	}
	switch len(validCmds) {
	case 0:
		return nil
	case 1:
		return validCmds[0]
	default:
		return func() Msg {
			return T(validCmds)
		}
	}
}

func Every(duration time.Duration, fn func(time.Time) Msg) Cmd {
	n := time.Now()
	d := n.Truncate(duration).Add(duration).Sub(n)
	t := time.NewTimer(d)
	return func() Msg {
		ts := <-t.C
		t.Stop()
		for len(t.C) > 0 {
			<-t.C
		}
		return fn(ts)
	}
}

func Tick(d time.Duration, fn func(time.Time) Msg) Cmd {
	t := time.NewTimer(d)
	return func() Msg {
		ts := <-t.C
		t.Stop()
		for len(t.C) > 0 {
			<-t.C
		}
		return fn(ts)
	}
}

type windowSizeMsg struct{}

func RequestWindowSize() Msg {
	return windowSizeMsg{}
}

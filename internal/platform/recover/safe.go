package safe

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

const (
	firstRestart   = 250 * time.Millisecond
	restartCeiling = 30 * time.Second
	stateDirMode   = 0o755
	CrashPrefix    = "crash-"
	CrashSuffix    = ".log"
)

type Panic struct {
	Unit  string
	Value any
	Stack []byte
}

type Report func(Panic)

func (p Panic) Error() string {
	return fmt.Sprintf("%s panicked: %v", p.Unit, p.Value)
}

func (p Panic) Saved(dir string) string {
	err := os.MkdirAll(dir, stateDirMode)
	var file *os.File
	if err == nil {
		file, err = os.CreateTemp(dir, CrashPrefix+strings.ReplaceAll(p.Unit, " ", "-")+"-"+time.Now().Format("20060102-150405")+"-*"+CrashSuffix)
	}
	if err == nil {
		_, err = fmt.Fprintf(file, "%v\n\n%s", p.Value, p.Stack)
		err = errors.Join(err, file.Close())
	}
	if err != nil {
		return p.Error() + ", and its stack was not saved: " + err.Error()
	}
	return p.Error() + ", the stack is in " + file.Name()
}

func Call(report Report, unit string, fn func() error) (err error) {
	defer func() {
		value := recover()
		if value == nil {
			return
		}
		caught, rethrown := value.(Panic)
		if !rethrown {
			caught = Panic{Unit: unit, Value: value, Stack: debug.Stack()}
		}
		err = caught
		if report != nil {
			defer func() { _ = recover() }()
			report(caught)
		}
	}()
	return fn()
}

func ran(fn func()) func() error {
	return func() error {
		fn()
		return nil
	}
}

func Go(report Report, unit string, fn func()) {
	go func() { _ = Call(report, unit, ran(fn)) }()
}

func Fatal(report Report, unit string, quit func(error), fn func()) {
	go func() {
		if err := Call(report, unit, ran(fn)); err != nil {
			quit(err)
		}
	}()
}

func Loop(report Report, unit string, quit <-chan struct{}, fn func()) {
	for wait := firstRestart; Call(report, unit, ran(fn)) != nil; wait = min(wait*2, restartCeiling) {
		select {
		case <-quit:
			return
		case <-time.After(wait):
		}
	}
}

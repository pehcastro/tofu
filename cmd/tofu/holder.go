package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"time"

	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/safe"
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const holderUsage = `usage: tofu holder --dir PATH --session ID

runs one session for every tofu that follows it, with no window, and ends once
no tofu has followed it for a short grace. The app starts it; nobody needs to.
`

func holderArgs(args []string) (dir, id string, err error) {
	for index := 0; index+1 < len(args); index += 2 {
		switch args[index] {
		case "--dir":
			dir = args[index+1]
		case "--session":
			id = args[index+1]
		default:
			return "", "", fmt.Errorf("unknown argument %q", args[index])
		}
	}
	if dir == "" || id == "" || len(args)%2 != 0 {
		return "", "", errors.New("name --dir and --session, each with a value")
	}
	return dir, id, nil
}

func holderVerb(args []string, errOut io.Writer) int {
	dir, id, err := holderArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu holder: %v\n\n%s", err, holderUsage)
		return exitUsage
	}
	say := func(line string) {
		if line != "" {
			_, _ = fmt.Fprintln(errOut, time.Now().Format(time.DateTime)+" tofu holder: "+line)
		}
	}
	failed := func(err error) int {
		say(err.Error())
		return exitVerdict
	}
	if err := os.Chdir(dir); err != nil {
		return failed(err)
	}
	defer sys.HoldProjectOpen(dir)()
	_ = os.Setenv(host.HolderVariable, id)
	collectForALongLife()
	deck, err := readCassette(os.Getenv(cassetteVariable))
	if err != nil {
		return failed(err)
	}
	clock, err := driveClock(deck)
	if err != nil {
		return failed(err)
	}
	open := openAppWire
	if deck != nil {
		open = driveWire(deck)
	} else {
		held := holdStore()
		defer held.close()
		defer keepAccountsAlive(held, say, say)()
	}
	resumed, unrecorded, err := holderCarry(id)
	switch {
	case err != nil:
		return failed(fmt.Errorf("session %s does not read: %w", id, err))
	case resumed.Busy != nil:
		say(resumed.Busy.Error())
		return exitOK
	}
	launch := launchOf(dir, resumed, false)
	say(launch.note)
	places, err := sys.ResolvePlaces(".")
	if err != nil {
		return failed(err)
	}
	engine := &appEngine{dir: dir, open: open, tabs: launch.tabs, places: places}
	live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: cronChecker(dir), Resumed: resumed.hosted(), Unrecorded: unrecorded, Now: clock})
	for _, trouble := range troubles {
		say(trouble)
	}
	safe.Go(crashTold(dir, say), "holder events", func() {
		for event := range live.Events() {
			if event.Kind == host.EventNote || event.Kind == host.EventFailure {
				say(event.Text)
			}
		}
	})
	say("process " + strconv.Itoa(os.Getpid()) + " opened session " + id)
	held := live.HoldWhileAttached(konst.HolderGraceMillis * time.Millisecond)
	live.Close()
	engine.warm.Close()
	if held != nil {
		say(held.Error())
		return exitOK
	}
	for _, warning := range turn.EndSession(context.Background(), dir, live.ID(), sessionEndExit) {
		say(warning)
	}
	say(sessionEndLine(launch.registry, launch.registryErr))
	leaveShells(launch.registry)
	return exitOK
}

func holderCarry(id string) (resumed sessionResume, unrecorded bool, err error) {
	store, err := sessionstore.Open()
	if err != nil {
		return sessionResume{}, false, err
	}
	if _, err := store.Header(id); errors.Is(err, fs.ErrNotExist) {
		return sessionResume{Session: id}, true, nil
	}
	resumed, err = resumeOf(store, id)
	return resumed, false, err
}

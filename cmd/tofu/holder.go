package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"tofu/internal/host"
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func hostRuns(args []string) bool {
	return len(args) > 1 && args[0] == host.HubVerb && args[1] == host.HubRunVerb
}

func hubRunVerb(args []string, errOut io.Writer) int {
	say := func(line string) {
		if line != "" {
			_, _ = fmt.Fprintln(errOut, time.Now().Format(time.DateTime)+" tofu host: "+line)
		}
	}
	dir := "."
	if len(args) == 2 && args[0] == "--dir" {
		dir = args[1]
	} else if len(args) > 0 {
		_, _ = fmt.Fprintf(errOut, "tofu host: unknown argument %s\n\n%s", strconv.Quote(args[0]), projectHostUsage)
		return exitUsage
	}
	if err := runHub(dir, say); err != nil {
		say(err.Error())
		if !errors.Is(err, host.ErrHubRunning) {
			return exitVerdict
		}
	}
	return exitOK
}

func runHub(dir string, say func(string)) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	defer sys.HoldProjectOpen(dir)()
	collectForALongLife()
	deck, err := readCassette(os.Getenv(cassetteVariable))
	if err != nil {
		return err
	}
	clock, err := driveClock(deck)
	if err != nil {
		return err
	}
	open := openAppWire
	if deck != nil {
		open = driveWire(deck)
	} else {
		held := holdStore()
		defer held.close()
		defer keepAccountsAlive(held, say, say)()
	}
	launch := launchOf(dir, sessionResume{}, false)
	say(launch.note)
	defer leaveShells(launch.registry)
	places, err := sys.ResolvePlaces(dir)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	return host.RunHub(dir, crashTold(dir, say), say, func(id string, projectTurn *sync.Mutex) (*host.Host, func(), []string) {
		resumed, unrecorded, err := hostedCarry(id)
		switch {
		case err != nil:
			return nil, nil, []string{"it does not read: " + err.Error()}
		case resumed.Busy != nil:
			return nil, nil, []string{resumed.Busy.Error()}
		}
		engine := &appEngine{dir: dir, open: open, tabs: tools.NewBrowserTabs(home), places: places}
		live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: cronChecker(dir), Resumed: resumed.hosted(), Unrecorded: unrecorded, ProjectTurn: projectTurn, Now: clock})
		return live, func() {
			live.Close()
			engine.warm.Close()
			for _, warning := range turn.EndSession(context.Background(), dir, live.ID(), sessionEndExit) {
				say(warning)
			}
		}, troubles
	})
}

func hostedCarry(id string) (resumed sessionResume, unrecorded bool, err error) {
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

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"tofu/internal/host"
	"tofu/internal/safe"
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

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
	if launch.note != "" {
		say(launch.note)
	}
	places, err := sys.ResolvePlaces(dir)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	safe.Go(crashTold(dir, say), "session sweep", func() { sweepSessions(dir, say) })
	return host.RunHub(dir, crashTold(dir, say), say, func(id string, projectTurn *sync.Mutex) (*host.Host, func(), []string) {
		resumed, unrecorded, err := hostedCarry(id)
		switch {
		case err != nil:
			return nil, nil, []string{"it does not read: " + err.Error()}
		case resumed.Busy != nil:
			return nil, nil, []string{resumed.Busy.Error()}
		}
		shells := launch.registry
		if shells != nil {
			shells = shells.ForSession(id)
		}
		engine := &appEngine{dir: dir, open: open, tabs: tools.NewBrowserTabs(home), places: places}
		live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: shells, Check: cronChecker(dir), Resumed: resumed.hosted(), Unrecorded: unrecorded, ProjectTurn: projectTurn, Now: clock})
		return live, func() {
			live.Close()
			leaveShells(shells)
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

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	shellsverb "tofu/apps/cli/verbs/shells"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	sessionstore "tofu/internal/store/session"
	"tofu/internal/tools/registry"
)

func runHub(dir string, say func(string)) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	defer platformproject.HoldProjectOpen(dir)()
	collectForALongLife()
	deck, err := readCassette(os.Getenv(cassetteVariable))
	if err != nil {
		return err
	}
	clock, err := driveClock(deck)
	if err != nil {
		return err
	}
	if deck == nil {
		held := wiring.HoldStore()
		defer held.Close()
		defer wiring.KeepAccountsAlive(held, say, say)()
	}
	launch := launchOf(dir, sessionResume{}, false)
	if launch.note != "" {
		say(launch.note)
	}
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	told := wiring.CrashTold(dir, say)
	safe.Go(told, "session sweep", func() { sweepSessions(dir, say) })
	hostEnded := make(chan struct{})
	defer close(hostEnded)
	safe.Go(told, "scratch sweep", func() { shellsverb.SweepScratch(dir, say, hostEnded) })
	return host.RunHub(dir, told, say, func(id string, projectTurn *sync.Mutex) (*host.Host, func(), []string) {
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
		engine := &appEngine{dir: dir, open: deckOpener(deck), tabs: registry.NewBrowserTabs(home), places: places, arms: wiring.RunOpts{DoneArm: os.Getenv(doneReviewVariable)}}
		live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: shells, Check: shellsverb.CronChecker(dir), Resumed: resumed.hosted(), Unrecorded: unrecorded, ProjectTurn: projectTurn, Now: clock})
		return live, func() {
			live.Close()
			leaveShells(shells)
			engine.warm.Close()
			for _, warning := range loop.EndSession(context.Background(), dir, live.ID(), sessionEndExit) {
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

func wireOpener() wiring.Opener {
	read := sync.OnceValues(func() (*cassette, error) { return readCassette(os.Getenv(cassetteVariable)) })
	return func(opts wiring.RunOpts) (wiring.Wire, error) {
		deck, err := read()
		if err != nil {
			return wiring.Wire{}, err
		}
		return deckOpener(deck)(opts)
	}
}

func deckOpener(deck *cassette) wiring.Opener {
	if deck == nil {
		return wiring.OpenWire
	}
	return driveWire(deck)
}

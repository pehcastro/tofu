package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const driveDeskUsage = `usage: tofu drive desk [--out DIR] [--exe PATH] [--first-run STATE] [STEP...] [-- DESK ARGS]

opens the desk app in a home made fresh for the run, takes the steps, and
writes a screenshot once it has painted, one at each shot step and one at
the end, each once the window has held still, with a step log. It prints
the folder they are in and closes the desk. It builds nothing: it runs
--exe, or else the newest tofu-desktop build under $CARGO_TARGET_DIR or
apps/desk/target*.

steps:
  click:X,Y   click at X,Y in the window, in pixels from its top left
  type:TEXT   type TEXT into what has focus
  key:NAME    press enter, tab, escape, backspace, delete, space, up, down,
              left, right, home, end, pageup, pagedown, or f1 to f12
  wait:DUR    wait 500ms, 2s, any Go duration
  shot:NAME   write a screenshot named NAME

--out DIR writes into DIR, which must be empty or absent; without it the run
makes a new folder under the temp folder. The home is DIR/home: HOME,
USERPROFILE, APPDATA, LOCALAPPDATA and the XDG folders all point inside it,
so the run never reads or writes your home or your running desk.

--first-run no-tofu|no-account|no-project|ready sets DESK_FIRST_RUN, so a
fresh home can open past onboarding with ready.

Everything after -- goes to the desk as it is: -- --screen boardy opens that
screen, and -- FOLDER opens the desk on a project. The desk opens on a
Windows desktop made for the run, so its window never shows on your screen
or taskbar, and clicks and keys go to it as messages, never through the real
cursor or keyboard. Windows only for now.
`

const (
	deskPaintedLine      = "desk: first paint"
	deskFirstRunVariable = "DESK_FIRST_RUN"
	deskFirstShot        = "open"
	deskLastShot         = "end"
	deskStepLog          = "steps.log"
	deskHomeName         = "home"
	deskOutPrefix        = "tofu-drive-desk-"
)

type deskStep struct {
	verb, arg string
	at        image.Point
	pause     time.Duration
}

type deskPlan struct {
	out, exe, firstRun string
	steps              []deskStep
	deskArgs           []string
}

func deskFirstRuns() []string {
	return []string{"no-tofu", "no-account", "no-project", "ready"}
}

func parseDeskStep(word string) (deskStep, error) {
	verb, arg, found := strings.Cut(word, ":")
	step := deskStep{verb: verb, arg: arg}
	switch {
	case !found:
		return step, fmt.Errorf("step %q is not VERB:VALUE", word)
	case verb == "click":
		x, y, pair := strings.Cut(arg, ",")
		across, errX := strconv.Atoi(x)
		down, errY := strconv.Atoi(y)
		if !pair || errX != nil || errY != nil || across < 0 || down < 0 {
			return step, fmt.Errorf("click takes X,Y in pixels, not %q", arg)
		}
		step.at = image.Pt(across, down)
	case verb == "wait":
		pause, err := time.ParseDuration(arg)
		if err != nil || pause < 0 {
			return step, fmt.Errorf("wait takes a duration such as 500ms, not %q", arg)
		}
		step.pause = pause
	case verb == "key":
		if _, known := deskKeys()[arg]; !known {
			return step, fmt.Errorf("key %q is not one this drive presses", arg)
		}
	case verb == "shot":
		if arg == "" || strings.ContainsAny(arg, `/\:*?"<>|`) {
			return step, fmt.Errorf("shot takes a file name, not %q", arg)
		}
	case verb != "type":
		return step, fmt.Errorf("step %q is not click, type, key, wait or shot", verb)
	}
	return step, nil
}

func parseDeskPlan(args []string) (deskPlan, error) {
	var plan deskPlan
	for i := 0; i < len(args); i++ {
		name, value, joined := strings.Cut(args[i], "=")
		switch {
		case args[i] == "--":
			plan.deskArgs = args[i+1:]
			return plan, nil
		case name == "--out" || name == "--exe" || name == "--first-run":
			if !joined && i+1 < len(args) {
				i++
				value = args[i]
			}
			switch {
			case value == "":
				return plan, errors.New(name + " takes a value")
			case name == "--out":
				plan.out = value
			case name == "--exe":
				plan.exe = value
			case !slices.Contains(deskFirstRuns(), value):
				return plan, fmt.Errorf("--first-run takes %s, not %q", strings.Join(deskFirstRuns(), ", "), value)
			default:
				plan.firstRun = value
			}
		default:
			step, err := parseDeskStep(args[i])
			if err != nil {
				return plan, err
			}
			plan.steps = append(plan.steps, step)
		}
	}
	return plan, nil
}

func deskFolder(named string) (string, error) {
	if named == "" {
		return os.MkdirTemp("", deskOutPrefix)
	}
	folder, err := filepath.Abs(named)
	if err != nil {
		return "", err
	}
	if real, err := os.UserHomeDir(); err != nil || strings.HasPrefix(folder+string(filepath.Separator), filepath.Join(real, sys.StateDirName)+string(filepath.Separator)) {
		return "", fmt.Errorf("--out %s is inside your real %s, or your real home cannot be found, so the drive does not start", named, sys.StateDirName)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(folder); err != nil || len(entries) > 0 {
		return "", fmt.Errorf("--out %s is not empty, so its home would not be fresh: name a new folder", named)
	}
	return folder, nil
}

func freshHomeEnv(home string) []string {
	moved := map[string]string{
		"HOME": home, "USERPROFILE": home,
		"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME": filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
	}
	env := slices.DeleteFunc(os.Environ(), func(pair string) bool {
		name, _, _ := strings.Cut(pair, "=")
		_, replaced := moved[strings.ToUpper(name)]
		return replaced
	})
	for name, value := range moved {
		env = append(env, name+"="+value)
	}
	return env
}

func writePNG(path string, picture image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, picture); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func stillFrame(desk *deskWindow) (*image.RGBA, string, error) {
	settle := time.Duration(konst.MotionSettleMillisDefault) * time.Millisecond
	ready := time.Duration(konst.MotionReadyTimeoutMillis) * time.Millisecond
	last, err := desk.capture()
	started, unchanged := time.Now(), time.Now()
	for err == nil && time.Since(unchanged) < settle {
		if time.Since(started) > ready {
			return last, "still moving after " + ready.String(), nil
		}
		time.Sleep(time.Duration(konst.DriveSettleMillis) * time.Millisecond)
		var next *image.RGBA
		if next, err = desk.capture(); err == nil && !bytes.Equal(next.Pix, last.Pix) {
			last, unchanged = next, time.Now()
		}
	}
	return last, "still for " + settle.String(), err
}

func driveDeskVerb(args []string, out, errOut io.Writer) int {
	started := time.Now()
	fail := func(err error) int {
		_, _ = fmt.Fprintf(errOut, "tofu drive desk: %v\n", err)
		return exitUsage
	}
	plan, err := parseDeskPlan(args)
	if err != nil {
		return fail(err)
	}
	exe, built, err := sys.NewestDesk("", plan.exe)
	if err != nil {
		return fail(err)
	}
	folder, err := deskFolder(plan.out)
	if err != nil {
		return fail(err)
	}
	home := filepath.Join(folder, deskHomeName)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fail(err)
	}
	stepLog, err := os.Create(filepath.Join(folder, deskStepLog))
	if err != nil {
		return fail(err)
	}
	defer func() { _ = stepLog.Close() }()
	record := func(line string) {
		_, _ = fmt.Fprintf(stepLog, "%8.3fs %s\n", time.Since(started).Seconds(), line)
	}
	record("desk " + exe + " built " + built.Format(time.DateTime) + " args " + strings.Join(plan.deskArgs, " "))
	record("home " + home + " first run " + cmp.Or(plan.firstRun, "as the home finds it"))
	env := freshHomeEnv(home)
	if plan.firstRun != "" {
		env = append(env, deskFirstRunVariable+"="+plan.firstRun)
	}
	desk, err := openDesk(exe, plan.deskArgs, env, folder)
	if err != nil {
		record("refused: " + err.Error())
		return fail(err)
	}
	var shots []string
	shoot := func(name string) error {
		picture, still, err := stillFrame(desk)
		if err != nil {
			return err
		}
		path := filepath.Join(folder, fmt.Sprintf("%02d-%s.png", len(shots)+1, name))
		if err := writePNG(path, picture); err != nil {
			return err
		}
		shots = append(shots, path)
		record("shot " + path + " " + picture.Bounds().Size().String() + " " + still)
		return nil
	}
	record("window open")
	err = shoot(deskFirstShot)
	for _, step := range plan.steps {
		if err != nil {
			break
		}
		record(step.verb + ":" + step.arg)
		switch step.verb {
		case "click":
			err = desk.click(step.at)
		case "type":
			err = desk.typeText(step.arg)
		case "key":
			err = desk.press(deskKeys()[step.arg])
		case "wait":
			time.Sleep(step.pause)
		case "shot":
			err = shoot(step.arg)
		}
	}
	if err == nil {
		err = shoot(deskLastShot)
	}
	closed := desk.close(time.Duration(konst.DriveTimeoutMillis) * time.Millisecond)
	record("closed: " + closed)
	if err != nil {
		record("failed: " + err.Error())
	}
	_, _ = fmt.Fprintf(out, "desk    %s, built %s\nhome    %s\n", exe, built.Format(time.DateTime), home)
	for _, shot := range shots {
		_, _ = fmt.Fprintf(out, "shot    %s\n", shot)
	}
	_, _ = fmt.Fprintf(out, "log     %s\nclosed  %s\nfolder  %s\n", filepath.Join(folder, deskStepLog), closed, folder)
	if err != nil {
		return fail(err)
	}
	return exitOK
}

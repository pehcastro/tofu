package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	deskBinary     = "tofu-desktop"
	deskPanicked   = "panicked at"
	paintedColours = 16
)

var deskFlows = []flow{
	{12, "desk on a fresh home opens onboarding and speaks to the built tofu", false, deskOnboarding},
	{13, "desk past onboarding opens a fresh project on work and a click closes the sidebar", false, deskWork},
	{14, "desk opens the boardy screen on a fresh home", false, deskBoardy},
}

func deskBuild(src string) (string, time.Time, error) {
	name := deskBinary
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	found, _ := filepath.Glob(filepath.Join(src, "apps", "desk", "target*", "*", name))
	if target := os.Getenv("CARGO_TARGET_DIR"); target != "" {
		shared, _ := filepath.Glob(filepath.Join(target, "*", name))
		found = append(found, shared...)
	}
	var newest string
	var built time.Time
	for _, path := range found {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(built) {
			newest, built = path, info.ModTime()
		}
	}
	if newest == "" {
		return "", built, fmt.Errorf("no %s under %s or $CARGO_TARGET_DIR/*: build the desk from HEAD", name, filepath.Join(src, "apps", "desk", "target*"))
	}
	return newest, built, nil
}

func deskStale(repo, desk string, built time.Time) (string, error) {
	committed, err := git(repo, "log", "-1", "--format=%cI", "HEAD", "--", "apps/desk")
	if err != nil {
		return "", err
	}
	at, err := time.Parse(time.RFC3339, committed)
	if err != nil || built.After(at) {
		return "", err
	}
	return fmt.Sprintf("the desk build %s, built %s, is older than the last apps/desk commit at %s, so it cannot be the stamped tree: build the desk from HEAD", desk, built.Format(time.DateTime), at.Format(time.DateTime)), nil
}

func (s session) driveDesk(args ...string) (string, []string, error) {
	if s.desk == "" {
		return "", nil, errors.New("no desk build to drive; the desk build line above says why")
	}
	out := filepath.Join(s.dir, "desk")
	cmd := s.tofu(append([]string{"drive", "desk", "--exe", s.desk, "--out", out}, args...)...)
	cmd.Env = append(cmd.Env, "DESK_TOFU="+s.exe)
	printed, err := cmd.CombinedOutput()
	if kept := s.keep("desk", string(printed)); kept != nil {
		return "", nil, kept
	}
	said, _ := os.ReadFile(filepath.Join(out, "desk.log"))
	shots, _ := filepath.Glob(filepath.Join(out, "*.png"))
	switch {
	case err != nil:
		return string(said), shots, errors.New(failure(string(printed), err))
	case bytes.Contains(said, []byte(deskPanicked)):
		return string(said), shots, fmt.Errorf("the desk panicked: %s", lineWith(string(said), deskPanicked))
	}
	return string(said), shots, nil
}

func deskSaid(said string, marks ...string) error {
	for _, mark := range marks {
		if !strings.Contains(said, mark) {
			return fmt.Errorf("desk.log does not say %q", mark)
		}
	}
	return nil
}

func painted(shot string) error {
	file, err := os.Open(shot)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	picture, err := png.Decode(file)
	if err != nil {
		return err
	}
	seen := map[[4]uint32]bool{}
	bounds := picture.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y && len(seen) < paintedColours; y++ {
		for x := bounds.Min.X; x < bounds.Max.X && len(seen) < paintedColours; x++ {
			r, g, b, a := picture.At(x, y).RGBA()
			seen[[4]uint32{r, g, b, a}] = true
		}
	}
	if len(seen) < paintedColours {
		return fmt.Errorf("%s holds %d colours, so the desk painted nothing", filepath.Base(shot), len(seen))
	}
	return nil
}

func deskOnboarding(s session) (string, error) {
	said, shots, err := s.driveDesk()
	if err != nil {
		return "", err
	}
	if err := deskSaid(said, "routed to onboarding", "speaks tofu.host/", "in "+s.project); err != nil {
		return "", err
	}
	if err := painted(shots[len(shots)-1]); err != nil {
		return "", err
	}
	return lineWith(said, "routed to onboarding") + "; " + lineWith(said, "speaks tofu.host/"), nil
}

func deskWork(s session) (string, error) {
	said, shots, err := s.driveDesk("--first-run", "ready", "click:20,20", "shot:toggled", "--", s.project)
	if err != nil {
		return "", err
	}
	if err := deskSaid(said, "routed to work", "desk: project "+s.project, "sidebar closed"); err != nil {
		return "", err
	}
	if len(shots) < 2 {
		return "", fmt.Errorf("the drive kept %d screenshots, and the click needs one before and one after", len(shots))
	}
	if err := painted(shots[0]); err != nil {
		return "", err
	}
	before, errBefore := os.ReadFile(shots[0])
	after, errAfter := os.ReadFile(shots[1])
	if err := errors.Join(errBefore, errAfter); err != nil {
		return "", err
	}
	if bytes.Equal(before, after) {
		return "", fmt.Errorf("%s and %s are the same picture, so the click changed nothing on screen", filepath.Base(shots[0]), filepath.Base(shots[1]))
	}
	return lineWith(said, "routed to work") + "; the click closed the sidebar and changed the picture", nil
}

func deskBoardy(s session) (string, error) {
	said, shots, err := s.driveDesk("--first-run", "ready", "--", "--screen", "boardy")
	if err != nil {
		return "", err
	}
	if err := deskSaid(said, "routed to boardy", "boardy: open first paint"); err != nil {
		return "", err
	}
	if err := painted(shots[len(shots)-1]); err != nil {
		return "", err
	}
	return lineWith(said, "boardy: open first paint"), nil
}

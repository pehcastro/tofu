package report

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"tofu/internal/konst"
)

func Generate(noun string, render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	body, err := render(hostname(), date)
	if err != nil {
		return err
	}
	body += fmt.Sprintf("\nMeasured on tofu %s at %s.\n", konst.Version, revision())
	path := filepath.Join("..", fmt.Sprintf("report-%s.md", date))
	if err := Write(path, []byte(body), 0o644, noun); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}

func revision() string {
	info, built := debug.ReadBuildInfo()
	if !built {
		return "a revision the build did not stamp"
	}
	commit, dirty := "", ""
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value[:min(8, len(setting.Value))]
		case "vcs.modified":
			if setting.Value == "true" {
				dirty = " with uncommitted changes"
			}
		}
	}
	if commit == "" {
		return "a revision the build did not stamp"
	}
	return commit + dirty
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

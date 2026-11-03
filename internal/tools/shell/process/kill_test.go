package shell

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAKillIsRoutedOnlyWhenEveryTargetIsAPidTofuStarted(t *testing.T) {
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	pids := map[string]string{}
	for _, name := range []string{"bash-1", "bash-2"} {
		started, err := registry.Start(t.TempDir(), name, "sleep 60", "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registry.Kill(name) })
		pids[name] = strconv.Itoa(started.PID)
	}
	spelled := strings.NewReplacer("{own}", pids["bash-1"], "{two}", pids["bash-2"], "{other}", strconv.Itoa(os.Getpid()))
	for command, want := range map[string]string{
		"Stop-Process -Id {own} -Force -ErrorAction SilentlyContinue":                      "bash-1",
		`powershell -NoProfile -Command "Stop-Process -Id {own} -Force -ErrorAction Stop"`: "bash-1",
		`pwsh -NoProfile -ExecutionPolicy Bypass -c "Stop-Process -Id {own}"`:              "bash-1",
		"Stop-Process -Id {own},{two} -Confirm:$false":                                     "bash-1 bash-2",
		"taskkill /PID {own} /T /F":                                                        "bash-1",
		"taskkill //PID {own} //F":                                                         "bash-1",
		"kill {own}":                                                                       "bash-1",
		"kill -9 {own}":                                                                    "bash-1",
		`bash -c "kill -9 {own}"`:                                                          "bash-1",
		`sh -c 'kill {own}'`:                                                               "bash-1",
		"cmd /c taskkill /PID {own} /F":                                                    "bash-1",
		"Stop-Process -Id {other}":                                                         "",
		"Stop-Process -Id {own},{other}":                                                   "",
		"kill {other}":                                                                     "",
		"Stop-Process -Id {own}; Remove-Item x":                                            "",
		`powershell -Command "Stop-Process -Id {own}; Remove-Item x"`:                      "",
		"kill {own} && rm -rf x":                                                           "",
		`bash -c "kill {own}" ; rm x`:                                                      "",
		"bash script.sh -c kill {own}":                                                     "",
		"Stop-Process -Id {own} -ErrorAction ;rm":                                          "",
		"Stop-Process -Id {own} -ErrorVariable oops":                                       "",
		"Stop-Process -Id {own} -WhatIf":                                                   "",
		"taskkill /S host /PID {own} /F":                                                   "",
		"taskkill /FI PID {own}":                                                           "",
		"kill -l {own}":                                                                    "",
		"kill /{own}":                                                                      "",
		"kill $PID":                                                                        "",
		"kill $(cat pidfile)":                                                              "",
		"kill %1":                                                                          "",
		"echo {own} | xargs kill":                                                          "",
		"Get-Process -Id {own} | Stop-Process":                                             "",
	} {
		var got []string
		for _, one := range registry.Owning(spelled.Replace(command)) {
			got = append(got, one.Name)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%q routes to %q, want %q", command, strings.Join(got, " "), want)
		}
	}
}

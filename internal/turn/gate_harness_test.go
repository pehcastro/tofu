package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"tofu/internal/sys"
)

func TestPersonOnlyBashHarnessCases(t *testing.T) {
	project := t.TempDir()
	scratch, err := sys.ScratchRootAt(project)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	shot := filepath.ToSlash(filepath.Join(project, ".local", "desk-app", "shots", "x"))
	inScratch := filepath.ToSlash(filepath.Join(scratch, "sessions", "a", "home"))
	outside := filepath.ToSlash(filepath.Join(filepath.Dir(project), "elsewhere-home"))
	unread, read := "HOME", "USERPROFILE"
	if runtime.GOOS != "windows" {
		unread, read = read, unread
	}
	cases := []struct {
		name    string
		command string
		asks    bool
	}{
		{"owner read of a harness file beside an unknown program", `apps/desk/target-agent-271/motion/tofu-desk.exe 2>&1 | grep -iE "screen" | head -3; grep -h projectManagement .tofu/settings.json 2>/dev/null`, false},
		{"cat a harness file", `cat .tofu/settings.json`, false},
		{"unknown program writing only to a device", `go run ./cmd/x 2>/dev/null; grep -h k .claude/settings.json`, false},
		{"settings set in a home inside the project", "HOME=" + shot + " USERPROFILE=" + shot + " tofu settings set --scope global projectManagement boardy", false},
		{"settings set in a home inside the scratch", "HOME=" + inScratch + " USERPROFILE=" + inScratch + " tofu settings set projectManagement boardy", false},
		{"project scoped settings set", `tofu settings set --scope project oneTurnPerProject false`, true},
		{"plain settings set", `tofu settings set gatePrompt auto`, true},
		{"settings set through rtk", `rtk tofu settings set gatePrompt auto`, true},
		{"project scope under a throwaway home still writes the project", "HOME=" + shot + " USERPROFILE=" + shot + " tofu settings set --scope project k v", true},
		{"home outside the project and scratch", "HOME=" + outside + " USERPROFILE=" + outside + " tofu settings set k v", true},
		{"home set to the real home", "HOME=" + filepath.ToSlash(home) + " USERPROFILE=" + filepath.ToSlash(home) + " tofu settings set k v", true},
		{"home escaping the project with dot dot", "HOME=" + shot + "/../../../.. USERPROFILE=" + shot + "/../../../.. tofu settings set k v", true},
		{"home relative escaping the project", "HOME=../x USERPROFILE=../x tofu settings set k v", true},
		{"only the home variable this os does not read", unread + "=" + shot + " tofu settings set k v", true},
		{"the read home variable set but the other outside", read + "=" + outside + " " + unread + "=" + shot + " tofu settings set k v", true},
		{"home from an expansion", `HOME=$HOME USERPROFILE=$USERPROFILE tofu settings set k v`, true},
		{"throwaway settings set then a real one", "HOME=" + shot + " USERPROFILE=" + shot + " tofu settings set k v; tofu settings set k v", true},
		{"real settings set then a throwaway one", "tofu settings set k v && HOME=" + shot + " USERPROFILE=" + shot + " tofu settings set k v", true},
		{"hooks trust", `tofu hooks trust`, true},
		{"hooks trust under a throwaway home", "HOME=" + shot + " USERPROFILE=" + shot + " tofu hooks trust", true},
		{"redirect into the settings file", `echo {} > .tofu/settings.json`, true},
		{"read then redirect into the hooks file", `cat x.json > .tofu/hooks.json`, true},
		{"copy over a claude settings file", `cp x .claude/settings.json`, true},
		{"sed in place on the settings file", `sed -i s/a/b/ .tofu/settings.json`, true},
		{"unknown program handed a harness file", `python fix.py .tofu/settings.json`, true},
		{"unknown program handed a harness file through rtk", `rtk node fix.js .tofu/hooks/trusted.json`, true},
		{"unparsed write naming a harness file", `cat .tofu/settings.json > $OUT`, true},
		{"redirect into the home settings file", `echo {} > ~/.tofu/settings.json`, true},
		{"unknown program handed a harness file after a cd", `cd .tofu && python x.py settings.json`, true},
		{"unknown program handed a hooks file after a nested cd", `cd .tofu; cd hooks; node fix.js trusted.json`, true},
		{"unknown program handed a claude file after a cd with a semicolon", `cd .claude; node x.js settings.local.json`, true},
		{"unknown program handed a harness file after a non-literal cd", `cd $DIR && python x.py settings.json`, true},
		{"unknown program handed a harness file after a cd home", `cd ~ && python x.py .tofu/settings.json`, true},
		{"unknown program after a non-literal cd with no harness name", `cd $DIR && go test ./...`, false},
		{"read a harness file after a cd", `cd .tofu && cat settings.json`, false},
		{"unknown program after a cd away from the harness", `cd apps/desk && cargo build 2>/dev/null`, false},
	}
	for _, c := range cases {
		args, _ := json.Marshal(map[string]string{"command": c.command})
		got := PersonOnly(project, GateRequest{Tool: bashToolName, Args: args})
		t.Logf("person-only=%v  %s", got, c.name)
		if got != c.asks {
			t.Errorf("%s: person-only %v, want %v: %s", c.name, got, c.asks, c.command)
		}
	}
}

func TestPersonOnlyOwnerSessionCommands(t *testing.T) {
	project := t.TempDir()
	shot := filepath.ToSlash(filepath.Join(project, ".local", "desk-app", "shots", "x"))
	for _, command := range []string{
		`apps/desk/target-agent-271/motion/tofu-desk.exe 2>&1 | grep -iE "screen" | head -3; grep -h projectManagement .tofu/settings.json 2>/dev/null`,
		`tofu settings set --scope project oneTurnPerProject false`,
		"HOME=" + shot + " USERPROFILE=" + shot + " tofu settings set --scope global projectManagement boardy",
	} {
		args, _ := json.Marshal(map[string]string{"command": command})
		t.Logf("person-only=%v  %s", PersonOnly(project, GateRequest{Tool: bashToolName, Args: args}), command)
	}
}

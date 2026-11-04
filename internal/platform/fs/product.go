package fs

import (
	"cmp"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

const ProductSymbol = `^func nearestCommand\(`

func ProductPackage(root string) (string, error) {
	found, err := exec.Command("git", "-C", root, "grep", "-l", "-E", ProductSymbol, "--", "*.go").Output()
	files := strings.Fields(string(found))
	if err != nil || len(files) != 1 {
		return "", fmt.Errorf("symbol %s is in %d Go files under %s, not one, so the tofu package is unknown: %s", ProductSymbol, len(files), root, cmp.Or(strings.Join(files, " "), fmt.Sprint(err)))
	}
	return "./" + path.Dir(files[0]), nil
}

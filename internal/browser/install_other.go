//go:build !windows

package browser

import (
	"fmt"
	"runtime"
)

func register(string, string) error {
	return fmt.Errorf("installing the Chrome native host is not supported on %s", runtime.GOOS)
}

func unregister(string) error {
	return fmt.Errorf("uninstalling the Chrome native host is not supported on %s", runtime.GOOS)
}

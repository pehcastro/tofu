//go:build !windows

package project

import "path/filepath"

func finalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

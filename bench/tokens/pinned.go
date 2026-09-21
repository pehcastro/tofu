package tokens

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const PinnedCommit = "e3c805d2b86b5e289305f21a88bcde837b37c838"

var ErrNoPinnedTree = errors.New("bench/tokens: the pinned commit is not in this checkout, so no glob call can be replayed")

func pinnedTree(repoRoot string) (string, func(), error) {
	if err := exec.Command("git", "-C", repoRoot, "cat-file", "-e", PinnedCommit+"^{commit}").Run(); err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrNoPinnedTree, err)
	}
	dir, err := os.MkdirTemp("", "bench-tokens-pinned-")
	if err != nil {
		return "", nil, err
	}
	remove := func() { _ = os.RemoveAll(dir) }
	if err := extractPinned(repoRoot, dir); err != nil {
		remove()
		return "", nil, err
	}
	return dir, remove, nil
}

func extractPinned(repoRoot, into string) error {
	archive := exec.Command("git", "-C", repoRoot, "archive", "--format=tar", PinnedCommit)
	stream, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return fmt.Errorf("%w: %v", ErrNoPinnedTree, err)
	}
	reader := tar.NewReader(stream)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeDir:
			continue
		case tar.TypeReg:
			name := filepath.Join(into, filepath.FromSlash(header.Name))
			if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
				return err
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			if err := os.WriteFile(name, body, 0o600); err != nil {
				return err
			}
		default:
			return fmt.Errorf("bench/tokens: %s in %s is neither a file nor a directory", header.Name, PinnedCommit)
		}
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return err
	}
	return archive.Wait()
}

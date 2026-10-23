package host

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

const hubTempDirPrefix = "tofu-"

func hubSocketOf(home, name string) (string, error) {
	sum := sha256.Sum256([]byte(home))
	return socketWithinLimit(filepath.Join(os.TempDir(), hubTempDirPrefix+hex.EncodeToString(sum[:])[:hubShortKey], name+".sock"))
}

func throughSocketDir(socket string, use func(string) error) error {
	return use(socket)
}

package boardy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const counterFileName = "counter"

func (s Store) next(key, parent string) (string, error) {
	dir, lock, prefix := s.BoardDir(key), counterFileName, key
	if parent != "" {
		ref, err := SplitID(parent)
		if err != nil || ref.Key != key || ref.Child != 0 {
			return "", errors.Join(err, fmt.Errorf("%s cannot hold a sub-ticket on %s: a parent is a top-level ticket of the same board", parent, key))
		}
		if _, err := s.Get(parent); err != nil {
			return "", err
		}
		dir, lock, prefix = filepath.Join(dir, TicketsDirName, parent), parent+"-"+counterFileName, parent
	}
	var number int
	err := s.locked(key, lock, func() error {
		path := filepath.Join(dir, counterFileName)
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if last := strings.TrimSpace(string(raw)); last != "" {
			if number, err = strconv.Atoi(last); err != nil {
				return err
			}
		}
		number++
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return writeAtomic(path, []byte(strconv.Itoa(number)+"\n"))
	})
	return prefix + "-" + strconv.Itoa(number), err
}

package boardy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const counterFileName = "counter"

func (s Store) next(key string) (int, error) {
	var number int
	err := s.locked(key, counterFileName, func() error {
		path := filepath.Join(s.BoardDir(key), counterFileName)
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
		return writeAtomic(path, []byte(strconv.Itoa(number)+"\n"))
	})
	return number, err
}

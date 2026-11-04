package project

import (
	"strings"

	"golang.org/x/sys/windows"
)

const finalPathAsDOSDrive = 0

func finalPath(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), finalPathAsDOSDrive)
		if err != nil {
			return "", err
		}
		if int(length) < len(buffer) {
			final := windows.UTF16ToString(buffer[:length])
			if unc, found := strings.CutPrefix(final, `\\?\UNC\`); found {
				return `\\` + unc, nil
			}
			return strings.TrimPrefix(final, `\\?\`), nil
		}
		buffer = make([]uint16, length)
	}
}

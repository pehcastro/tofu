package host

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func lockHub(path string) (*os.File, bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	return os.NewFile(uintptr(handle), path), false, nil
}

func hubSocketOf(home, name string) (string, error) {
	return tempSocketOf(home, name)
}

func throughSocketDir(socket string, use func(string) error) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	owner, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := owner.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(filepath.Dir(socket), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return err
	}
	return use(socket)
}

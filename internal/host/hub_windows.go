package host

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func errorOutputInto(file *os.File) (restore func(), err error) {
	was, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		return nil, err
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(file.Fd())); err != nil {
		return nil, err
	}
	return func() { _ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, was) }, nil
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

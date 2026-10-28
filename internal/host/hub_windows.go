package host

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

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

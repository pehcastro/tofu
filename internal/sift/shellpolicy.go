package sift

import "io/fs"

const ShellSchema = "shell_sift"

type ShellPolicy foreignPolicy

func LoadShellPolicy(shipped fs.FS, name string) (ShellPolicy, error) {
	pol, err := loadForeignPolicy(shipped, name, ShellSchema)
	return ShellPolicy(pol), err
}

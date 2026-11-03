package sift

import "io/fs"

const ShellSchema = "shell_sift"

type ShellRule foreignRule

func LoadShellRule(shipped fs.FS, ref string) (ShellRule, error) {
	r, err := loadForeignRule(shipped, ref, ShellSchema)
	return ShellRule(r), err
}

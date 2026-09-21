package sift

import "io/fs"

const PageSchema = "page_sift"

type PageRule foreignRule

func LoadPageRule(shipped fs.FS, ref string) (PageRule, error) {
	r, err := loadForeignRule(shipped, ref, PageSchema)
	return PageRule(r), err
}

package sift

import "io/fs"

const PageSchema = "page_sift"

type PagePolicy foreignPolicy

func LoadPagePolicy(shipped fs.FS, name string) (PagePolicy, error) {
	pol, err := loadForeignPolicy(shipped, name, PageSchema)
	return PagePolicy(pol), err
}

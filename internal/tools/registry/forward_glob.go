package registry

import (
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

type Glob = glob.Glob

const ignoredWalkDescription = glob.IgnoredWalkDescription

func NewGlob(dir string) (Glob, error) { return glob.NewGlob(dir) }

func ListPaths(dir string) ([]string, error) { return glob.ListPaths(dir) }

func withNote(content, note string) string { return glob.WithNote(content, note) }

type listing struct {
	under string
	files []string
	sizes []int64
	bytes int64
	note  string
}

func filesUnder(root contract.Root, under string, includeIgnored bool) (listing, error) {
	listed, err := glob.FilesUnder(root, under, includeIgnored)
	return listing{under: listed.Under, files: listed.Files, sizes: listed.Sizes, bytes: listed.Bytes, note: listed.Note}, err
}

func repairPath(root contract.Root, requested string, withDirectories bool) (string, string, error) {
	return glob.Repair(root, requested, withDirectories)
}

func replaceOnce(before, shown, wanted, becomes string) (string, string, error) {
	return glob.ReplaceOnce(before, shown, wanted, becomes)
}

package sys

import (
	"io/fs"
	"os"
)

type Layer struct {
	Name   string
	Origin string
	FS     fs.FS
}

func DirLayer(name string, dir string) Layer {
	return Layer{Name: name, Origin: dir, FS: os.DirFS(dir)}
}

func Layers(shipped fs.FS, sub string) ([]Layer, error) {
	home, err := HomeConfigDir()
	if err != nil {
		return nil, err
	}
	project, err := ProjectStateDir()
	if err != nil {
		return nil, err
	}
	return []Layer{
		{Name: "library", Origin: Join("library", sub), FS: shipped},
		DirLayer("global", Join(home, sub)),
		DirLayer("project", Join(project, sub)),
	}, nil
}

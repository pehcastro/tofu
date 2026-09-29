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

func Layers(shipped fs.FS, sub, dir string) ([]Layer, error) {
	project := StateDir(dir)
	if dir == "" {
		var err error
		if project, err = ProjectConfigDir(); err != nil {
			return nil, err
		}
	}
	layers := []Layer{{Name: "library", Origin: Join("library", sub), FS: shipped}}
	if home, homeless := HomeConfigDir(); homeless == nil {
		layers = append(layers, DirLayer("global", Join(home, sub)))
	}
	return append(layers, DirLayer("project", Join(project, sub))), nil
}

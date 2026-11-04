package wiring

import (
	"path/filepath"

	"tofu/internal/models/llm/catalog"
	"tofu/internal/platform/fs"
)

const (
	registryFileName = "model-windows.json"
	TokensPerMillion = 1_000_000
)

func RegistryPath() (string, error) {
	dir, err := fs.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryFileName), nil
}

func ModelRegistry() (catalog.Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return catalog.ShippedRegistry()
	}
	return catalog.RegistryAt(path)
}

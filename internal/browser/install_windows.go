package browser

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

func register(hostKey, manifestPath string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, hostKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	return errors.Join(key.SetStringValue("", manifestPath), key.Close())
}

func unregister(hostKey string) error {
	err := registry.DeleteKey(registry.CURRENT_USER, hostKey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

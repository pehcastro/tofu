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

func browserManifests(string) []string { return nil }

func nativeHosts(_, hostsKey string) []NativeHost {
	chrome := NativeHost{Browser: "Chrome"}
	if key, err := registry.OpenKey(registry.CURRENT_USER, hostsKey+`\`+HostName, registry.QUERY_VALUE); err == nil {
		chrome.Manifest, _, _ = key.GetStringValue("")
		_ = key.Close()
	}
	return []NativeHost{chrome}
}

func unregister(hostKey string) error {
	err := registry.DeleteKey(registry.CURRENT_USER, hostKey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

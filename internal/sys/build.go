package sys

import "runtime/debug"

const UnknownRevision = "unknown"

func BuildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return UnknownRevision
	}
	revision := UnknownRevision
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision != UnknownRevision && dirty {
		return revision + "-dirty"
	}
	return revision
}

const DevelVersion = "dev"

func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return DevelVersion
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		return DevelVersion
	}
	return version
}

func GoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return UnknownRevision
	}
	return info.GoVersion
}

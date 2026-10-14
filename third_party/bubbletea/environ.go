package tea

import uv "github.com/charmbracelet/ultraviolet"

type EnvMsg uv.Environ

func (msg EnvMsg) Getenv(key string) (v string) {
	return uv.Environ(msg).Getenv(key)
}

func (msg EnvMsg) LookupEnv(key string) (s string, v bool) {
	return uv.Environ(msg).LookupEnv(key)
}

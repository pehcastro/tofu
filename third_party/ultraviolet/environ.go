package uv

import (
	"strings"
)

type Environ []string

func (p Environ) Getenv(key string) (v string) {
	v, _ = p.LookupEnv(key)
	return
}

func (p Environ) LookupEnv(key string) (s string, v bool) {
	for i := len(p) - 1; i >= 0; i-- {
		if strings.HasPrefix(p[i], key+"=") {
			s = strings.TrimPrefix(p[i], key+"=")
			v = true
			break
		}
	}
	return
}

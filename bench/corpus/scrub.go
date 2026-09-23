package corpus

import "tofu/internal/secret"

func Scrub(text string) string { return secret.Scrub(text) }

func LeaksIn(text string) []string { return secret.LeaksIn(text) }

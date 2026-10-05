package corpus

import "tofu/internal/secret"

func Scrub(text string) string { return secret.Machine().Scrub(text) }

func LeaksIn(text string) []string { return secret.Machine().LeaksIn(text) }

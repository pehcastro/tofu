package corpus

import "tofu/internal/platform/credential"

func Scrub(text string) string { return secret.Machine().Scrub(text) }

func LeaksIn(text string) []string { return secret.Machine().LeaksIn(text) }

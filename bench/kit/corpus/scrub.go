package corpus

import "tofu/internal/platform/credential"

func Scrub(text string) string { return credential.Machine().Scrub(text) }

func LeaksIn(text string) []string { return credential.Machine().LeaksIn(text) }

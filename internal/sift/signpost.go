package sift

import (
	"strings"

	"tofu/internal/konst"
)

func Signpost(part Part) Mark {
	flat := strings.TrimSpace(part.Text)
	if strings.HasPrefix(flat, "#") {
		return Mark{Reason: "a markdown heading"}
	}
	if strings.HasSuffix(flat, ":") && len(strings.Fields(flat)) <= konst.SiftSignpostWordCap {
		return Mark{Reason: "a short line ending in a colon"}
	}
	return Mark{Keep: true}
}

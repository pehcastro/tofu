package sift

import "tofu/internal/konst"

func Length(part Part) Mark {
	if part.Words() < konst.SiftReadWorthWordFloor {
		return Mark{Reason: "under the word floor"}
	}
	return Mark{Keep: true}
}

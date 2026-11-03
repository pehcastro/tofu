package widget

import "testing"

func TestMaskingAMaskedKeyKeepsItsTail(t *testing.T) {
	for _, key := range []string{"sk-or-v1-made-up-000000000000Q9W4", "short"} {
		once := Mask(key)
		if twice := Mask(once); twice != once {
			t.Errorf("a key masked once reads %q and masked again reads %q", once, twice)
		}
	}
}

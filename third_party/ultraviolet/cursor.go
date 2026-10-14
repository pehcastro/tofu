package uv

type CursorShape int

const (
	CursorBlock CursorShape = iota
	CursorUnderline
	CursorBar
)

func (s CursorShape) Encode(blink bool) int {

	s = (s * 2) + 1
	if !blink {
		s++
	}
	return int(s)
}

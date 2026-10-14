package tea

type Position struct{ X, Y int }

type CursorPositionMsg struct {
	X, Y int
}

type CursorShape int

const (
	CursorBlock CursorShape = iota
	CursorUnderline
	CursorBar
)

type requestCursorPosMsg struct{}

func RequestCursorPosition() Msg {
	return requestCursorPosMsg{}
}

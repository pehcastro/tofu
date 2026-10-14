package uv

import "github.com/charmbracelet/x/ansi"

type Window struct {
	*Buffer

	method *WidthMethod
	parent *Window
	bounds Rectangle
}

var (
	_ Screen   = (*Window)(nil)
	_ Drawable = (*Window)(nil)
)

func (w *Window) HasParent() bool {
	return w.parent != nil
}

func (w *Window) Parent() *Window {
	return w.parent
}

func (w *Window) MoveTo(x, y int) {
	size := w.bounds.Size()
	w.bounds.Min.X = x
	w.bounds.Min.Y = y
	w.bounds.Max.X = x + size.X
	w.bounds.Max.Y = y + size.Y
}

func (w *Window) MoveBy(dx, dy int) {
	w.bounds.Min.X += dx
	w.bounds.Min.Y += dy
	w.bounds.Max.X += dx
	w.bounds.Max.Y += dy
}

func (w *Window) Clone() *Window {
	return w.CloneArea(w.Buffer.Bounds())
}

func (w *Window) CloneArea(area Rectangle) *Window {
	clone := new(Window)
	clone.Buffer = w.Buffer.CloneArea(area)
	clone.parent = w.parent
	clone.method = w.method
	clone.bounds = area
	return clone
}

func (w *Window) Resize(width, height int) {

	if w.parent == nil || w.Buffer != w.parent.Buffer {
		w.Buffer.Resize(width, height)
	}
	w.bounds.Max.X = w.bounds.Min.X + width
	w.bounds.Max.Y = w.bounds.Min.Y + height
}

func (w *Window) WidthMethod() WidthMethod {
	return *w.method
}

func (w *Window) Bounds() Rectangle {
	return w.bounds
}

func (w *Window) NewWindow(x, y, width, height int) *Window {
	return newWindow(w, x, y, width, height, w.method, false)
}

func (w *Window) NewView(x, y, width, height int) *Window {
	return newWindow(w, x, y, width, height, w.method, true)
}

func NewWindow(width, height int, method WidthMethod) *Window {
	if method == nil {
		method = ansi.WcWidth
	}
	return newWindow(nil, 0, 0, width, height, &method, false)
}

func (w *Window) SetWidthMethod(method WidthMethod) {
	w.method = &method
}

func newWindow(parent *Window, x, y, width, height int, method *WidthMethod, view bool) *Window {
	w := new(Window)
	if view {
		w.Buffer = parent.Buffer
	} else {
		w.Buffer = NewBuffer(width, height)
	}
	w.parent = parent
	w.method = method
	w.bounds = Rect(x, y, width, height)
	return w
}

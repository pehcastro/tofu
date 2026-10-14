package uv

import (
	"os"
	"sync"

	"github.com/charmbracelet/x/term"
)

type SizeNotifier struct {
	C <-chan os.Signal

	f   term.File
	sig chan os.Signal
	m   sync.Mutex
}

func NewSizeNotifier(f term.File) *SizeNotifier {
	if f == nil {
		panic("no file set")
	}
	sig := make(chan os.Signal)
	return &SizeNotifier{
		f:   f,
		sig: sig,
		C:   sig,
	}
}

func (n *SizeNotifier) Start() error {
	return n.start()
}

func (n *SizeNotifier) Stop() error {
	return n.stop()
}

func (n *SizeNotifier) GetWindowSize() (cells Size, pixels Size, err error) {
	return n.getWindowSize()
}

func (n *SizeNotifier) GetSize() (width, height int, err error) {
	n.m.Lock()
	defer n.m.Unlock()

	width, height, err = term.GetSize(n.f.Fd())
	if err != nil {
		return 0, 0, err
	}

	return width, height, nil
}

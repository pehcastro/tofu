//go:build windows

package tea

func (p *Program) listenForResize(done chan struct{}) {
	close(done)
}

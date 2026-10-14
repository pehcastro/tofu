package clipboard

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfUnicodetext = 13
	gmemMoveable  = 0x0002
)

var (
	user32                     = syscall.NewLazyDLL("user32")
	isClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	openClipboard              = user32.NewProc("OpenClipboard")
	closeClipboard             = user32.NewProc("CloseClipboard")
	emptyClipboard             = user32.NewProc("EmptyClipboard")
	getClipboardData           = user32.NewProc("GetClipboardData")
	setClipboardData           = user32.NewProc("SetClipboardData")

	kernel32     = syscall.NewLazyDLL("kernel32")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalFree   = kernel32.NewProc("GlobalFree")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	lstrcpy      = kernel32.NewProc("lstrcpyW")
)

func waitOpenClipboard() error {
	limit := time.Now().Add(time.Second)
	var r uintptr
	var err error
	for time.Now().Before(limit) {
		r, _, err = openClipboard.Call(0)
		if r != 0 {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

func readAll() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if formatAvailable, _, err := isClipboardFormatAvailable.Call(cfUnicodetext); formatAvailable == 0 {
		return "", err
	}
	if err := waitOpenClipboard(); err != nil {
		return "", err
	}
	h, _, err := getClipboardData.Call(cfUnicodetext)
	if h == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}
	l, _, err := globalLock.Call(h)
	if l == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}
	text := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(l))[:])
	r, _, err := globalUnlock.Call(h)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}
	closed, _, err := closeClipboard.Call()
	if closed == 0 {
		return "", err
	}
	return text, nil
}

func writeAll(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := waitOpenClipboard(); err != nil {
		return err
	}
	r, _, err := emptyClipboard.Call(0)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	data := syscall.StringToUTF16(text)
	h, _, err := globalAlloc.Call(gmemMoveable, uintptr(len(data)*int(unsafe.Sizeof(data[0]))))
	if h == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	defer func() {
		if h != 0 {
			_, _, _ = globalFree.Call(h)
		}
	}()
	l, _, err := globalLock.Call(h)
	if l == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	r, _, err = lstrcpy.Call(l, uintptr(unsafe.Pointer(&data[0])))
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	r, _, err = globalUnlock.Call(h)
	if r == 0 && err.(syscall.Errno) != 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	r, _, err = setClipboardData.Call(cfUnicodetext, h)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	h = 0
	closed, _, err := closeClipboard.Call()
	if closed == 0 {
		return err
	}
	return nil
}

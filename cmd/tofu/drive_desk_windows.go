package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"tofu/internal/konst"
)

const (
	wmClose             = 0x0010
	wmKeyDown           = 0x0100
	wmKeyUp             = 0x0101
	wmChar              = 0x0102
	wmMouseMove         = 0x0200
	wmLeftButtonDown    = 0x0201
	wmLeftButtonUp      = 0x0202
	mkLeftButton        = 0x0001
	keyUpTransition     = 0xC0000001
	gwOwner             = 4
	pwClientOnly        = 1
	pwRenderFullContent = 2
	perMonitorAwareV2   = ^uintptr(3)
	desktopAllAccess    = 0x01FF
	vkF1                = 0x70
	functionKeys        = 12
)

func deskKeys() map[string]uintptr {
	keys := map[string]uintptr{
		"backspace": 0x08, "tab": 0x09, "enter": 0x0D, "escape": 0x1B, "space": 0x20,
		"pageup": 0x21, "pagedown": 0x22, "end": 0x23, "home": 0x24,
		"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28, "delete": 0x2E,
	}
	for n := range functionKeys {
		keys["f"+strconv.Itoa(n+1)] = uintptr(vkF1 + n)
	}
	return keys
}

type deskWindow struct {
	process *os.Process
	exited  chan error
	desktop uintptr
	window  uintptr
	listed  uintptr
	user32  *windows.LazyDLL
	gdi32   *windows.LazyDLL
}

type clientRect struct {
	left, top, right, bottom int32
}

type bitmapHeader struct {
	size             uint32
	width, height    int32
	planes, bitCount uint16
	_                [6]uint32
}

func call(dll *windows.LazyDLL, name string, args ...uintptr) (uintptr, error) {
	result, _, err := dll.NewProc(name).Call(args...)
	if result == 0 {
		return 0, fmt.Errorf("%s failed: %w", name, err)
	}
	return result, nil
}

func openDesk(exe string, args, env []string, folder string) (*deskWindow, error) {
	desk := &deskWindow{user32: windows.NewLazySystemDLL("user32.dll"), gdi32: windows.NewLazySystemDLL("gdi32.dll"), exited: make(chan error, 1)}
	_, _ = call(desk.user32, "SetProcessDpiAwarenessContext", perMonitorAwareV2)
	name, _ := windows.UTF16PtrFromString(deskOutPrefix + filepath.Base(folder))
	desktop, _, err := desk.user32.NewProc("CreateDesktopW").Call(uintptr(unsafe.Pointer(name)), 0, 0, 0, desktopAllAccess, 0)
	if desktop == 0 {
		return nil, fmt.Errorf("CreateDesktopW failed: %w", err)
	}
	desk.desktop = desktop
	said := filepath.Join(folder, "desk.log")
	log, err := os.Create(said)
	if err != nil {
		desk.close(0)
		return nil, err
	}
	defer func() { _ = log.Close() }()
	_ = windows.SetHandleInformation(windows.Handle(log.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT)
	startup := windows.StartupInfo{Desktop: name, Flags: windows.STARTF_USESTDHANDLES, StdOutput: windows.Handle(log.Fd()), StdErr: windows.Handle(log.Fd())}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	line, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, args...)))
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	var started windows.ProcessInformation
	if err := windows.CreateProcess(nil, line, nil, nil, true, windows.CREATE_UNICODE_ENVIRONMENT, &block[0], nil, &startup, &started); err != nil {
		desk.close(0)
		return nil, err
	}
	_ = windows.CloseHandle(started.Thread)
	desk.process, err = os.FindProcess(int(started.ProcessId))
	_ = windows.CloseHandle(started.Process)
	if err != nil {
		desk.close(0)
		return nil, err
	}
	go func() {
		_, err := desk.process.Wait()
		desk.exited <- err
	}()
	desk.listed = windows.NewCallback(func(window, _ uintptr) uintptr {
		var pid uint32
		_, _ = windows.GetWindowThreadProcessId(windows.HWND(window), &pid)
		owner, _, _ := desk.user32.NewProc("GetWindow").Call(window, gwOwner)
		if int(pid) == desk.process.Pid && owner == 0 && windows.IsWindowVisible(windows.HWND(window)) {
			desk.window = window
			return 0
		}
		return 1
	})
	timeout := time.Duration(konst.DriveTimeoutMillis) * time.Millisecond
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(time.Duration(konst.DriveSettleMillis) * time.Millisecond) {
		select {
		case <-desk.exited:
			desk.process = nil
			desk.close(0)
			return nil, errors.New("the desk quit before it opened a window: " + said + " has what it said")
		default:
		}
		told, _ := os.ReadFile(said)
		if _, err := desk.find(); err == nil && strings.Contains(string(told), deskPaintedLine) {
			return desk, nil
		}
	}
	desk.close(0)
	return nil, errors.New("the desk did not open a window and log " + strconv.Quote(deskPaintedLine) + " within " + timeout.String() + ": " + said + " has what it said")
}

func (d *deskWindow) find() (uintptr, error) {
	if d.window != 0 && windows.IsWindow(windows.HWND(d.window)) {
		return d.window, nil
	}
	d.window = 0
	_, _, _ = d.user32.NewProc("EnumDesktopWindows").Call(d.desktop, d.listed, 0)
	if d.window == 0 {
		return 0, errors.New("the desk has no window open")
	}
	return d.window, nil
}

func (d *deskWindow) post(message, wParam, lParam uintptr) error {
	window, err := d.find()
	if err == nil {
		_, err = call(d.user32, "PostMessageW", window, message, wParam, lParam)
	}
	return err
}

func (d *deskWindow) click(at image.Point) error {
	point := uintptr(at.Y)<<16 | uintptr(at.X)&0xFFFF
	for _, message := range [][2]uintptr{{wmMouseMove, 0}, {wmLeftButtonDown, mkLeftButton}, {wmLeftButtonUp, 0}} {
		if err := d.post(message[0], message[1], point); err != nil {
			return err
		}
		time.Sleep(time.Duration(konst.DriveSettleMillis) * time.Millisecond)
	}
	return nil
}

func (d *deskWindow) typeText(text string) error {
	for _, unit := range utf16.Encode([]rune(text)) {
		if err := d.post(wmChar, uintptr(unit), 1); err != nil {
			return err
		}
	}
	return nil
}

func (d *deskWindow) press(key uintptr) error {
	if err := d.post(wmKeyDown, key, 1); err != nil {
		return err
	}
	return d.post(wmKeyUp, key, keyUpTransition)
}

func (d *deskWindow) capture() (*image.RGBA, error) {
	window, err := d.find()
	if err != nil {
		return nil, err
	}
	var rect clientRect
	if got, _, err := d.user32.NewProc("GetClientRect").Call(window, uintptr(unsafe.Pointer(&rect))); got == 0 {
		return nil, fmt.Errorf("GetClientRect failed: %w", err)
	}
	width, height := rect.right-rect.left, rect.bottom-rect.top
	if width <= 0 || height <= 0 {
		return nil, errors.New("the desk window has no client area")
	}
	memory, err := call(d.gdi32, "CreateCompatibleDC", 0)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = call(d.gdi32, "DeleteDC", memory) }()
	header := bitmapHeader{width: width, height: -height, planes: 1, bitCount: 32}
	header.size = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	bitmap, _, err := d.gdi32.NewProc("CreateDIBSection").Call(memory, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection failed: %w", err)
	}
	defer func() { _, _ = call(d.gdi32, "DeleteObject", bitmap) }()
	previous, _ := call(d.gdi32, "SelectObject", memory, bitmap)
	_, printErr := call(d.user32, "PrintWindow", window, memory, pwClientOnly|pwRenderFullContent)
	_, _ = call(d.gdi32, "GdiFlush")
	_, _ = call(d.gdi32, "SelectObject", memory, previous)
	if printErr != nil {
		return nil, printErr
	}
	picture := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	copy(picture.Pix, unsafe.Slice((*byte)(bits), len(picture.Pix)))
	for i := 0; i < len(picture.Pix); i += 4 {
		picture.Pix[i], picture.Pix[i+2], picture.Pix[i+3] = picture.Pix[i+2], picture.Pix[i], 0xFF
	}
	return picture, nil
}

func (d *deskWindow) close(grace time.Duration) string {
	said := "the desk never started"
	if d.process != nil {
		if d.window != 0 {
			_ = d.post(wmClose, 0, 0)
		}
		select {
		case <-d.exited:
			said = "the desk quit on close"
		case <-time.After(grace):
			_ = d.process.Kill()
			<-d.exited
			said = "the desk did not quit within " + grace.String() + ", so it was killed"
		}
	}
	if d.desktop != 0 {
		_, _ = call(d.user32, "CloseDesktop", d.desktop)
	}
	return said
}

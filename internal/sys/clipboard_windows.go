//go:build windows

package sys

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clipboardOpenAttempts      = 5
	clipboardOpenBackoffMillis = 20
)

const (
	userLibrary   = "user32.dll"
	memoryLibrary = "kernel32.dll"
	shellLibrary  = "shell32.dll"
)

const (
	movableMemory           = 0x0002
	formatDIB               = 8
	formatUnicodeText       = 13
	formatFileDrop          = 15
	portableNetworkGraphics = "PNG"
	fileDropCountQuery      = 0xFFFFFFFF
	noOwnerWindow           = 0
)

func winCall(library, name string, args ...uintptr) uintptr {
	result, _, _ := windows.NewLazySystemDLL(library).NewProc(name).Call(args...)
	return result
}

func ReadClipboard() (Clipboard, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return Clipboard{}, err
	}
	defer winCall(userLibrary, "CloseClipboard")

	if available(formatFileDrop) {
		return clipboardFiles()
	}
	if encoded := registeredFormat(portableNetworkGraphics); encoded != 0 && available(encoded) {
		return clipboardImage(encoded)
	}
	if available(formatDIB) {
		return clipboardBitmap()
	}
	if available(formatUnicodeText) {
		return clipboardText()
	}
	return Clipboard{Kind: ClipboardEmpty}, nil
}

func WriteClipboardText(text string) error {
	wide, err := windows.UTF16FromString(text)
	if err != nil {
		return errors.New("sys: the text holds a zero byte, which the windows clipboard cannot carry")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return err
	}
	defer winCall(userLibrary, "CloseClipboard")
	if winCall(userLibrary, "EmptyClipboard") == 0 {
		return errors.New("sys: the clipboard would not let go of what it was holding")
	}
	size := uintptr(len(wide)) * unsafe.Sizeof(wide[0])
	handle := winCall(memoryLibrary, "GlobalAlloc", movableMemory, size)
	if handle == 0 {
		return errors.New("sys: windows would not give " + strconv.FormatUint(uint64(size), 10) + " bytes for the clipboard")
	}
	pointer := lockedMemory(handle)
	if pointer == nil {
		winCall(memoryLibrary, "GlobalFree", handle)
		return errors.New("sys: the clipboard block could not be locked for writing")
	}
	copy(unsafe.Slice((*uint16)(pointer), len(wide)), wide)
	winCall(memoryLibrary, "GlobalUnlock", handle)
	if winCall(userLibrary, "SetClipboardData", formatUnicodeText, handle) == 0 {
		winCall(memoryLibrary, "GlobalFree", handle)
		return errors.New("sys: windows refused the clipboard write")
	}
	return nil
}

func openClipboard() error {
	for attempt := range clipboardOpenAttempts {
		if winCall(userLibrary, "OpenClipboard", noOwnerWindow) != 0 {
			return nil
		}
		if attempt < clipboardOpenAttempts-1 {
			time.Sleep(clipboardOpenBackoffMillis * time.Millisecond)
		}
	}
	return errors.New("sys: another program is holding the clipboard")
}

func available(format uintptr) bool {
	return winCall(userLibrary, "IsClipboardFormatAvailable", format) != 0
}

func registeredFormat(name string) uintptr {
	wide, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	format := winCall(userLibrary, "RegisterClipboardFormatW", uintptr(unsafe.Pointer(wide)))
	runtime.KeepAlive(wide)
	return format
}

func lockedMemory(handle uintptr) unsafe.Pointer {
	address := winCall(memoryLibrary, "GlobalLock", handle)
	return *(*unsafe.Pointer)(unsafe.Pointer(&address))
}

func clipboardText() (Clipboard, error) {
	handle := winCall(userLibrary, "GetClipboardData", formatUnicodeText)
	if handle == 0 {
		return Clipboard{}, errors.New("sys: the clipboard would not hand over its text")
	}
	pointer := lockedMemory(handle)
	if pointer == nil {
		return Clipboard{}, errors.New("sys: the clipboard text could not be locked for reading")
	}
	defer winCall(memoryLibrary, "GlobalUnlock", handle)
	return Clipboard{Kind: ClipboardText, Text: windows.UTF16PtrToString((*uint16)(pointer))}, nil
}

func clipboardImage(format uintptr) (Clipboard, error) {
	body, err := clipboardBytes(format)
	if err != nil {
		return Clipboard{}, err
	}
	return Clipboard{Kind: ClipboardImage, PNG: body}, nil
}

func clipboardBitmap() (Clipboard, error) {
	raw, err := clipboardBytes(formatDIB)
	if err != nil {
		return Clipboard{}, err
	}
	body, err := pngFromDIB(raw)
	if err != nil {
		return Clipboard{}, err
	}
	return Clipboard{Kind: ClipboardImage, PNG: body}, nil
}

func clipboardBytes(format uintptr) ([]byte, error) {
	handle := winCall(userLibrary, "GetClipboardData", format)
	if handle == 0 {
		return nil, errors.New("sys: the clipboard would not hand over its image")
	}
	pointer := lockedMemory(handle)
	if pointer == nil {
		return nil, errors.New("sys: the clipboard image could not be locked for reading")
	}
	defer winCall(memoryLibrary, "GlobalUnlock", handle)
	length := winCall(memoryLibrary, "GlobalSize", handle)
	if length == 0 {
		return nil, errors.New("sys: the clipboard image is empty")
	}
	if length > ClipboardMaxBytes {
		return nil, errors.New("sys: the clipboard holds " + strconv.FormatUint(uint64(length), 10) + " bytes, past the paste ceiling")
	}
	return bytes.Clone(unsafe.Slice((*byte)(pointer), length)), nil
}

func clipboardFiles() (Clipboard, error) {
	drop := winCall(userLibrary, "GetClipboardData", formatFileDrop)
	if drop == 0 {
		return Clipboard{}, errors.New("sys: the clipboard would not hand over its file list")
	}
	count := winCall(shellLibrary, "DragQueryFileW", drop, fileDropCountQuery, 0, 0)
	if count == 0 {
		return Clipboard{}, errors.New("sys: the clipboard holds a file drop naming no file")
	}
	names := make([]string, 0, count)
	for index := range count {
		length := winCall(shellLibrary, "DragQueryFileW", drop, index, 0, 0)
		if length == 0 {
			continue
		}
		wide := make([]uint16, length+1)
		written := winCall(shellLibrary, "DragQueryFileW", drop, index, uintptr(unsafe.Pointer(&wide[0])), length+1)
		runtime.KeepAlive(wide)
		if written == 0 {
			continue
		}
		names = append(names, windows.UTF16ToString(wide[:written]))
	}
	return Clipboard{Kind: ClipboardFiles, Files: names}, nil
}

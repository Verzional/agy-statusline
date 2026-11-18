//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

type winCoord struct {
	X int16
	Y int16
}

type winSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type winConsoleScreenBufferInfo struct {
	Size              winCoord
	CursorPosition    winCoord
	Attributes        uint16
	Window            winSmallRect
	MaximumWindowSize winCoord
}

func detectTTYCols() int {
	pathPtr, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return 0
	}
	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil || handle == syscall.InvalidHandle {
		return 0
	}
	defer syscall.CloseHandle(handle)

	var info winConsoleScreenBufferInfo
	ret, _, _ := procGetConsoleScreenBufferInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&info)),
	)
	if ret == 0 {
		return 0
	}
	cols := int(info.Window.Right - info.Window.Left + 1)
	if cols <= 0 {
		return 0
	}
	return cols
}

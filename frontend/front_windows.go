package main

import "syscall"

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	user32              = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow    = kernel32.NewProc("GetConsoleWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
)

func bringToFront() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		setForegroundWindow.Call(hwnd)
	}
}

//go:build windows

package main

import (
	"log"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	wmClose         = 0x0010
	wmDestroy       = 0x0002
	wmCommand       = 0x0111
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203
	wmTray          = 0x8000 + 17
	nifMessage      = 0x00000001
	nifIcon         = 0x00000002
	nifTip          = 0x00000004
	nimAdd          = 0x00000000
	nimDelete       = 0x00000002
	nimSetVersion   = 0x00000004
	notifyVersion4  = 4
	mfString        = 0x00000000
	mfSeparator     = 0x00000800
	tpmRightButton  = 0x0002
	tpmReturnCmd    = 0x0100
	imageIcon       = 1
	lrLoadFromFile  = 0x0010
	lrDefaultSize   = 0x0040
	idiApplication  = 32512
	cmdOpen         = 1001
	cmdPauseAll     = 1002
	cmdResumeAll    = 1003
	cmdExit         = 1004
)

type point struct{ X, Y int32 }

type msg struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	shell32           = syscall.NewLazyDLL("shell32.dll")
	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pAppendMenuW      = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	pLoadImageW       = user32.NewProc("LoadImageW")
	pLoadIconW        = user32.NewProc("LoadIconW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	trayWindow        uintptr
	trayNotify        notifyIconData
	trayOnOpen        func()
	trayOnPause       func()
	trayOnResume      func()
	trayOnExit        func()
)

func startTray(iconPath string, onOpen, onPause, onResume, onExit func()) func() {
	ready := make(chan struct{})
	trayOnOpen, trayOnPause, trayOnResume, trayOnExit = onOpen, onPause, onResume, onExit
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		className, _ := syscall.UTF16PtrFromString("BoltDM.Tray.Window")
		title, _ := syscall.UTF16PtrFromString("BoltDM")
		hInstance, _, _ := pGetModuleHandleW.Call(0)
		wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: syscall.NewCallback(trayWndProc), HInstance: hInstance, LpszClassName: className}
		if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			log.Printf("tray register class failed: %v", e)
			close(ready)
			return
		}
		hwnd, _, e := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, hInstance, 0)
		if hwnd == 0 {
			log.Printf("tray window creation failed: %v", e)
			close(ready)
			return
		}
		trayWindow = hwnd
		hIcon := loadTrayIcon(iconPath)
		trayNotify = notifyIconData{CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: hwnd, UID: 1, UFlags: nifMessage | nifIcon | nifTip, UCallbackMessage: wmTray, HIcon: hIcon, UVersion: notifyVersion4}
		copy(trayNotify.SzTip[:], syscall.StringToUTF16("BoltDM — Download Manager"))
		if r, _, e := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&trayNotify))); r == 0 {
			log.Printf("tray icon add failed: %v", e)
		}
		pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&trayNotify)))
		close(ready)
		var m msg
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	<-ready
	return func() {
		if trayWindow != 0 {
			pPostMessageW.Call(trayWindow, wmClose, 0, 0)
		}
	}
}

func loadTrayIcon(path string) uintptr {
	if path != "" {
		p, _ := syscall.UTF16PtrFromString(path)
		if h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize); h != 0 {
			return h
		}
	}
	h, _, _ := pLoadIconW.Call(0, idiApplication)
	return h
}

func trayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTray:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			if trayOnOpen != nil {
				go trayOnOpen()
			}
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		dispatchTrayCommand(uint32(wParam & 0xffff))
		return 0
	case wmClose:
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayNotify)))
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		trayWindow = 0
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	appendMenu(menu, mfString, cmdOpen, "Open BoltDM")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdPauseAll, "Pause all downloads")
	appendMenu(menu, mfString, cmdResumeAll, "Resume all downloads")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdExit, "Exit BoltDM")
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWnd.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	if cmd != 0 {
		dispatchTrayCommand(uint32(cmd))
	}
}

func appendMenu(menu uintptr, flags, id uint32, text string) {
	if flags == mfSeparator {
		pAppendMenuW.Call(menu, uintptr(flags), 0, 0)
		return
	}
	p, _ := syscall.UTF16PtrFromString(text)
	pAppendMenuW.Call(menu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(p)))
}

func dispatchTrayCommand(cmd uint32) {
	switch cmd {
	case cmdOpen:
		if trayOnOpen != nil {
			go trayOnOpen()
		}
	case cmdPauseAll:
		if trayOnPause != nil {
			go trayOnPause()
		}
	case cmdResumeAll:
		if trayOnResume != nil {
			go trayOnResume()
		}
	case cmdExit:
		if trayOnExit != nil {
			go trayOnExit()
		}
	}
}

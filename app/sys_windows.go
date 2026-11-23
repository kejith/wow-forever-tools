//go:build windows

package main

// What the app needs from Windows itself: the game's process, the Explorer, the language of the system, child
// processes without a console window, and a picture of the window for the screenshots.

import (
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wowRunning looks for Wow.exe and tells WoW: Forever (its path has _forever_) from the other clients.
func wowRunning() map[string]bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return map[string]bool{"running": false, "unknown": true}
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	forever, other := false, false
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "Wow.exe") {
			continue
		}
		path := processPath(entry.ProcessID)
		if strings.Contains(strings.ToLower(path), "_forever_") {
			forever = true
		} else {
			other = true
		}
	}
	return map[string]bool{"running": forever, "otherClient": !forever && other}
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func openPath(p string) error {
	if _, err := os.Stat(p); err != nil {
		return err
	}
	// Explorer answers 1 even when it opened the folder.
	return exec.Command("explorer.exe", filepath.Clean(p)).Start()
}

// osLocale is the first language of the Windows user, e.g. "de-DE".
func osLocale() string {
	if langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(langs) > 0 {
		return langs[0]
	}
	return ""
}

const createNoWindow = 0x08000000

// hiddenProcess: a child that never shows a console window.
func hiddenProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// detachedProcess: a child that outlives the app (the installer of an update).
func detachedProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}

// ---------- screenshots ----------

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	gdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetClassName        = user32.NewProc("GetClassNameW")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procPrintWindow         = user32.NewProc("PrintWindow")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
)

type rect struct{ left, top, right, bottom int32 }

type bitmapInfoHeader struct {
	size                         uint32
	width, height                int32
	planes, bitCount             uint16
	compression, sizeImage       uint32
	xPelsPerMeter, yPelsPerMeter int32
	clrUsed, clrImportant        uint32
}

var (
	enumOnce     sync.Once
	enumCallback uintptr // made once: Windows keeps every callback for the life of the process
	enumFound    uintptr
)

// mainWindow is the app's own visible window of the class Wails registers.
func mainWindow() uintptr {
	enumOnce.Do(func() {
		pid := uint32(os.Getpid())
		enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
			var owner uint32
			procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
			if owner != pid {
				return 1
			}
			if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
				return 1
			}
			name := make([]uint16, 64)
			procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
			if windows.UTF16ToString(name) == "wailsWindow" {
				enumFound = hwnd
				return 0
			}
			return 1
		})
	})
	enumFound = 0
	procEnumWindows.Call(enumCallback, 0)
	return enumFound
}

// captureWindow is a picture of the window's content. PrintWindow with PW_RENDERFULLCONTENT also gets what
// WebView2 draws through DirectComposition, whether or not other windows cover it.
func captureWindow() (image.Image, error) {
	hwnd := mainWindow()
	if hwnd == 0 {
		return nil, errors.New("window not found")
	}
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := r.right-r.left, r.bottom-r.top
	if w <= 0 || h <= 0 {
		return nil, errors.New("window has no size")
	}
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	defer procDeleteDC.Call(dc)
	bmp, _, _ := procCreateCompatibleBmp.Call(screen, uintptr(w), uintptr(h))
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(dc, bmp)
	const pwClientOnly, pwRenderFullContent = 1, 2
	ok, _, _ := procPrintWindow.Call(hwnd, dc, pwClientOnly|pwRenderFullContent)
	procSelectObject.Call(dc, old)
	if ok == 0 {
		return nil, errors.New("PrintWindow failed")
	}
	info := bitmapInfoHeader{width: w, height: -h, planes: 1, bitCount: 32} // negative height: top-down rows
	info.size = uint32(unsafe.Sizeof(info))
	pixels := make([]byte, int(w)*int(h)*4)
	if n, _, _ := procGetDIBits.Call(dc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), 0); n == 0 {
		return nil, errors.New("GetDIBits failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(pixels); i += 4 { // BGRA -> RGBA, opaque
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], 255
	}
	return img, nil
}

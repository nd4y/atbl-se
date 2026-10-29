//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	script := filepath.Join(filepath.Dir(self), "windows", "Launcher.ps1")
	args := []string{"-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", script}
	if len(os.Args) > 1 && os.Args[1] == "--smoke" {
		args = append(args, "-Smoke")
	}
	cmd := exec.Command("powershell.exe", args...)
	// Suppress the console without passing SW_HIDE to the WinForms process.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		// Keep diagnostics outside the state directory: creating it here would
		// mask the legacy-state migration performed by the PowerShell launcher.
		if log, openErr := os.OpenFile(filepath.Join(local, "atblse-launcher-error.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); openErr == nil {
			defer log.Close()
			cmd.Stderr = log
		}
	}
	if err = cmd.Run(); err != nil {
		user32 := syscall.NewLazyDLL("user32.dll")
		msg, _ := syscall.UTF16PtrFromString("atblse could not start. Keep the windows folder next to the application and check Windows PowerShell availability.")
		title, _ := syscall.UTF16PtrFromString("atblse")
		user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x10)
	}
}

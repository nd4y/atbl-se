package core

import (
	"os"
	"syscall"
	"unsafe"
)

var moveStateFile = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func commitStateFile(temp, path string, replace bool) error {
	from, err := syscall.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uintptr(8) // MOVEFILE_WRITE_THROUGH
	if replace {
		flags |= 1
	} // MOVEFILE_REPLACE_EXISTING
	result, _, callErr := moveStateFile.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), flags)
	if result == 0 {
		return &os.LinkError{Op: "MoveFileExW", Old: temp, New: path, Err: callErr}
	}
	return nil
}

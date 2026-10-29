package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Only complete, flushed private files become visible. Existing state is never
// truncated. A failed commit is not success; a crash can leave an inert temp file.
func writeStateFile(path string, data []byte, replace bool) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return commitStateFile(f.Name(), path, replace)
}

func readSmallState(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("invalid state file %s; stop the server and restore a known-good backup", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("state file %s exceeds limit", filepath.Base(path))
	}
	return b, err
}

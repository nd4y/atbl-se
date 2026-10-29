//go:build !windows

package core

import (
	"os"
	"path/filepath"
)

func commitStateFile(temp, path string, replace bool) error {
	var err error
	if replace {
		err = os.Rename(temp, path)
	} else {
		err = os.Link(temp, path)
	}
	if err != nil {
		return err
	}
	if !replace {
		if err = os.Remove(temp); err != nil {
			return err
		}
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

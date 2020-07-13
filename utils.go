package main

import (
	"os"
	"time"
)

func fileExists(filename string) bool {
	fi, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return false
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		link, err := os.Readlink(filename)
		if err != nil {
			return false
		}
		fi, err = os.Stat(link)
		if os.IsNotExist(err) {
			return false
		}
		return !fi.IsDir()
	}
	return !fi.IsDir()
}

func fileDate(filename string) time.Time {
	fi, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return time.Now()
	}
	return fi.ModTime()
}

func mkDirIfNotExisting(dirName string) error {
	_, err := os.Stat(dirName)
	if os.IsNotExist(err) {
		err = os.MkdirAll(dirName, 0700)
	}
	return err
}

func stringSliceContains(slice []string, pb string) bool {
	for _, e := range slice {
		if e == pb {
			return true
		}
	}
	return false
}

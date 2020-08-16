package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kodabb/go-mtgmatcher/mtgmatcher"
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

func keyruneForCardSet(uuid string) string {
	uuids := mtgmatcher.GetUUIDs()
	co, found := uuids[uuid]
	if !found {
		return ""
	}

	sets := mtgmatcher.GetSets()
	set, found := sets[co.SetCode]
	if !found {
		return ""
	}

	keyrune := set.KeyruneCode
	if keyrune == "STAR" {
		keyrune = "PMEI"
	}

	rarity := co.Card.Rarity
	if co.SetCode == "TSB" {
		rarity = "timeshifted"
	}

	return fmt.Sprintf("ss-%s ss-%s", strings.ToLower(keyrune), rarity)
}

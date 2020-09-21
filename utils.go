package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
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

func keyruneForCardSet(cardId string) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	set, err := mtgmatcher.GetSet(co.SetCode)
	if err != nil {
		return ""
	}

	keyrune := set.KeyruneCode

	rarity := co.Card.Rarity
	if co.SetCode == "TSB" {
		rarity = "timeshifted"
	}
	out := fmt.Sprintf("ss-%s ss-%s", strings.ToLower(keyrune), rarity)

	if co.Foil {
		out += " ss-foil ss-grad"
	}

	return out
}

func scryfallImageURL(cardId string, small bool) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	version := "normal"
	if small {
		version = "small"
	}

	return fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=%s", strings.ToLower(co.SetCode), co.Card.Number, version)
}

func editionTitle(cardId string) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	foil := ""
	if co.Foil {
		foil = " Foil"
	}

	return fmt.Sprintf("%s -%s %s #%s", co.Edition, foil, strings.Title(co.Card.Rarity), co.Card.Number)
}

func insertNavBar(page string, nav []NavElem, extra []NavElem) []NavElem {
	i := 0
	for i = range nav {
		if nav[i].Name == page {
			break
		}
	}
	tail := nav[i:]
	nav = append(nav[:i], extra...)
	return append(nav, tail...)
}

func uuid2card(cardId string, smallImg bool) GenericCard {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return GenericCard{}
	}

	_, stocks := Infos["STKS"][cardId]

	variant := ""
	switch {
	case co.Card.HasPromoType("prerelease") && strings.HasSuffix(co.Edition, "Promos"):
		variant = "Prerelease"
	case co.Card.HasPromoType("promopack"):
		variant = "Promo Pack"
	case co.Card.HasPromoType("bundle"):
		variant = "Bundle Promo"
	case co.Card.HasPromoType("boosterfun"):
		switch {
		case co.Card.HasFrameEffect("showcase"):
			variant = "Showcase"
		case co.Card.HasFrameEffect("extendedart"):
			variant = "Extended Art"
		case co.Card.HasPromoType("godzillaseries"):
			variant = "Godzilla"
		case co.Card.BorderColor == "borderless":
			variant = "Borderless"
		}
	}

	return GenericCard{
		Name:      co.Card.Name,
		Edition:   co.Edition,
		SetCode:   co.SetCode,
		Number:    co.Card.Number,
		Variant:   variant,
		Foil:      co.Foil,
		Keyrune:   keyruneForCardSet(cardId),
		ImageURL:  scryfallImageURL(cardId, smallImg),
		Title:     editionTitle(cardId),
		Reserved:  co.Card.IsReserved,
		SearchURL: fmt.Sprintf("/search?q=%s s:%s cn:%s f:%t", co.Card.Name, co.SetCode, co.Card.Number, co.Foil),
		Stocks:    stocks,
	}
}

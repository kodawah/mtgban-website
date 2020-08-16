package main

import (
	"fmt"
	"strings"

	"github.com/kodabb/go-mtgban/mtgdb"
)

func ScryfallImageURL(card mtgdb.Card, small bool) (string, error) {
	var code, number string
	err := CardDB.QueryRow("SELECT setCode, number FROM cards WHERE mtgjsonV4Id = ?", strings.TrimSuffix(card.Id, "_f")).Scan(&code, &number)
	if err != nil {
		return "", err
	}
	version := "normal"
	if small {
		version = "small"
	}
	link := fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=%s", strings.ToLower(code), number, version)
	return link, nil
}

func KeyruneCodes(card mtgdb.Card) (string, string, error) {
	var name, keyrune, code, rarity, number string
	err := CardDB.QueryRow("SELECT setCode, rarity, number FROM cards WHERE mtgjsonV4Id = ?", strings.TrimSuffix(card.Id, "_f")).Scan(&code, &rarity, &number)
	if err != nil {
		return "", "", err
	}
	err = CardDB.QueryRow("SELECT name, keyruneCode FROM sets WHERE code = ?", code).Scan(&name, &keyrune)
	if err != nil {
		return "", "", err
	}

	if code == "TSB" {
		rarity = "timeshifted"
	}

	// Handle an idiosyncrasy between scryfall, keyrune, and mtgjson
	if keyrune == "STAR" {
		keyrune = "PMEI"
	}

	res := fmt.Sprintf("ss-%s ss-%s", strings.ToLower(keyrune), rarity)
	foil := ""
	if card.Foil {
		res += " ss-foil ss-grad"
		foil = " Foil"
	}
	long := fmt.Sprintf("%s -%s %s #%s", name, foil, strings.Title(rarity), number)
	return res, long, nil
}

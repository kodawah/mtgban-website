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

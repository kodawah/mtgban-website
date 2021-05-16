package main

import (
	"sort"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type EditionEntry struct {
	Name    string
	Code    string
	Date    time.Time
	Keyrune string
}

var categoryEdition = map[string]string{
	"archenemy":        "Boxed Sets",
	"box":              "Boxed Sets",
	"commander":        "Commander Decks",
	"core":             "Core Sets",
	"draft_innovation": "Draft Experiments",
	"duel_deck":        "Deck Series",
	"expansion":        "Expansions",
	"from_the_vault":   "From the Vault Sets",
	"funny":            "Funny Sets",
	"masterpiece":      "Boxed Sets",
	"masters":          "Reprint Sets",
	"memorabilia":      "Boxed Sets",
	"planechase":       "Boxed Sets",
	"premium_deck":     "Deck Series",
	"promo":            "Boxed Sets",
	"spellbook":        "Spellbook Series",
	"starter":          "Starter Sets",
}

var categoryOverrides = map[string]string{
	"CC1":  "Spellbook Series",
	"CM1":  "Boxed Sets",
	"CMB1": "Reprint Sets",
	"PTG":  "Boxed Sets",
}

var editionRenames = map[string]string{
	"Duel Decks Anthology: Elves vs. Goblins": "Duel Decks Anthology",
	"Magazine Inserts":                        "San Diego Comic-Con",
	"Mystery Booster Playtest Cards":          "Mystery Booster Convention Edition",
	"Mystery Booster Retail Edition Foils":    "Mystery Booster Retail Edition",
	"World Championship Decks 1997":           "World Championship Decks",
}

var editionSkips = map[string]string{
	"Chronicles Japanese":    "",
	"Legends Italian":        "",
	"The Dark Italian":       "",
	"Rivals Quick Start Set": "",
}

func getSealedEditions(pageVars *PageVars) {
	sets := mtgmatcher.GetSets()

	listEditions := map[string][]EditionEntry{}
	for _, set := range sets {
		if set.SealedProduct == nil {
			continue
		}

		_, found := editionSkips[set.Name]
		if found {
			continue
		}

		date, err := time.Parse("2006-01-02", set.ReleaseDate)
		if err != nil {
			continue
		}

		category, found := categoryEdition[set.Type]
		if !found {
			category = set.Type
		}
		rename, found := categoryOverrides[set.Code]
		if found {
			category = rename
		}

		name := set.Name
		rename, found = editionRenames[name]
		if found {
			name = rename
		}

		listEditions[category] = append(listEditions[category], EditionEntry{
			Name:    name,
			Code:    set.Code,
			Date:    date,
			Keyrune: strings.ToLower(set.KeyruneCode),
		})
	}

	for key := range listEditions {
		sort.Slice(listEditions[key], func(i, j int) bool {
			return listEditions[key][i].Date.After(listEditions[key][j].Date)
		})
		pageVars.EditionSort = append(pageVars.EditionSort, key)
	}

	sort.Slice(pageVars.EditionSort, func(i, j int) bool {
		return listEditions[pageVars.EditionSort[i]][0].Date.After(listEditions[pageVars.EditionSort[j]][0].Date)
	})

	pageVars.EditionList = listEditions
}

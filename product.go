package main

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
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
	"CC1":  "spellbook",
	"CM1":  "box",
	"CMB1": "masters",
	"PTG":  "box",
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

		setType := set.Type
		rename, found := categoryOverrides[set.Code]
		if found {
			setType = rename
		}
		category, found := categoryEdition[setType]
		if !found {
			category = set.Type
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

// Check if it makes sense to keep two keep foil and nonfoil separate
func combineFinish(setCode string) bool {
	set, err := mtgmatcher.GetSet(setCode)
	if err != nil {
		return false
	}

	setType := set.Type
	rename, found := categoryOverrides[setCode]
	if found {
		setType = rename
	}
	switch setType {
	case "commander",
		"box",
		"duel_deck",
		"from_the_vault",
		"masterpiece",
		"memorabilia",
		"promo":
		return true
	}

	return false
}

func runSealedAnalysis() {
	log.Println("Running set analysis")

	var tcgInventory mtgban.InventoryRecord
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Shorthand == TCG_LOW {
			tcgInventory, _ = seller.Inventory()
		}
	}

	var ckBuylist mtgban.BuylistRecord
	for _, vendor := range Vendors {
		if vendor != nil && vendor.Info().Shorthand == "CK" {
			ckBuylist, _ = vendor.Buylist()
		}
	}

	inv := map[string]float64{}
	invFoil := map[string]float64{}
	bl := map[string]float64{}
	blFoil := map[string]float64{}

	uuids := mtgmatcher.GetUUIDs()
	for uuid, co := range uuids {
		// Determine whether to keep prices separated or combine them
		useFoil := co.Foil && !combineFinish(co.SetCode)

		entriesBl, found := ckBuylist[uuid]
		if !found {
			switch co.Rarity {
			case "mythic":
				if useFoil {
					blFoil[co.SetCode] += 0.30
				} else {
					bl[co.SetCode] += 0.30
				}
			case "rare":
				if useFoil {
					blFoil[co.SetCode] += 0.30
				} else {
					bl[co.SetCode] += 0.10
				}
			case "common", "uncommon":
				if useFoil {
					blFoil[co.SetCode] += 0.05
				} else {
					bl[co.SetCode] += 0.005
				}
			default:
				if co.IsPromo {
					if useFoil {
						blFoil[co.SetCode] += 0.05
					} else {
						bl[co.SetCode] += 0.05
					}
				} else if mtgmatcher.IsBasicLand(co.Name) {
					if useFoil {
						blFoil[co.SetCode] += 0.10
					} else {
						bl[co.SetCode] += 0.01
					}
				}
			}
		} else {
			if useFoil {
				blFoil[co.SetCode] += entriesBl[0].BuyPrice
			} else {
				bl[co.SetCode] += entriesBl[0].BuyPrice
			}
		}

		entriesInv, found := tcgInventory[uuid]
		if found {
			if useFoil {
				invFoil[co.SetCode] += entriesInv[0].Price
			} else {
				inv[co.SetCode] += entriesInv[0].Price
			}
		}
	}

	totalValueByTcgLow := mtgban.InventoryRecord{}
	for code, price := range inv {
		totalValueByTcgLow[code] = append(totalValueByTcgLow[code], mtgban.InventoryEntry{
			Price: price,
		})
	}
	Infos["TotalValueByTcgLow"] = totalValueByTcgLow

	totalFoilValueByTcgLow := mtgban.InventoryRecord{}
	for code, price := range invFoil {
		totalFoilValueByTcgLow[code] = append(totalFoilValueByTcgLow[code], mtgban.InventoryEntry{
			Price: price,
		})
	}
	Infos["TotalFoilValueByTcgLow"] = totalFoilValueByTcgLow

	totalValueByBuylist := mtgban.InventoryRecord{}
	for code, price := range bl {
		totalValueByBuylist[code] = append(totalValueByBuylist[code], mtgban.InventoryEntry{
			Price: price,
		})
	}
	Infos["TotalValueBuylist"] = totalValueByBuylist

	totalFoilValueByBuylist := mtgban.InventoryRecord{}
	for code, price := range blFoil {
		totalFoilValueByBuylist[code] = append(totalFoilValueByBuylist[code], mtgban.InventoryEntry{
			Price: price,
		})
	}
	Infos["TotalFoilValueBuylist"] = totalFoilValueByBuylist
}

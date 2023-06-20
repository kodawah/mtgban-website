package main

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
	"github.com/mtgban/go-mtgban/mtgmatcher/mtgjson"
)

type EditionEntry struct {
	Name    string
	Code    string
	Date    time.Time
	Keyrune string
	Size    int
	FmtDate string
	Special bool
	ShowFin bool
	HasReg  bool
	HasFoil bool
}

var categoryEdition = map[string]string{
	"archenemy":        "Boxed Sets",
	"arsenal":          "Commander Supplements",
	"box":              "Boxed Sets",
	"commander":        "Commander Decks",
	"core":             "Core Sets",
	"draft_innovation": "Draft Experiments",
	"duel_deck":        "Deck Series",
	"expansion":        "Expansions",
	"from_the_vault":   "From the Vault Sets",
	"funny":            "Funny Sets",
	"game":             "Standalone Game",
	"masterpiece":      "Boxed Sets",
	"masters":          "Reprint Sets",
	"memorabilia":      "Boxed Sets",
	"planechase":       "Boxed Sets",
	"premium_deck":     "Deck Series",
	"promo":            "Boxed Sets",
	"starter":          "Starter Sets",
	"vanguard":         "Boxed Sets",
}

var categoryOverrides = map[string]string{
	"CMB1": "masters",
	"CMB2": "masters",
	"PTG":  "box",
	"SS1":  "arsenal",
	"SS2":  "arsenal",
	"SS3":  "arsenal",
	"HOP":  "game",
	"PC2":  "game",
	"PCA":  "game",
	"E02":  "game",
	"ARC":  "game",
}

var editionRenames = map[string]string{
	"Duel Decks Anthology: Elves vs. Goblins": "Duel Decks Anthology",
	"Media Inserts":                        "San Diego Comic-Con",
	"Modern Horizons 1 Timeshifts":         "Modern Horizons",
	"Mystery Booster Playtest Cards 2019":  "Mystery Booster Convention Edition 2019",
	"Mystery Booster Playtest Cards 2021":  "Mystery Booster Convention Edition 2021",
	"Mystery Booster Retail Edition Foils": "Mystery Booster Retail Edition",
	"World Championship Decks 1997":        "World Championship Decks",
	"Judge Gift Cards 2014":                "Judge Gift Cards",
}

var sealedEditionSkips = map[string]string{
	"Chronicles Japanese":    "",
	"Legends Italian":        "",
	"The Dark Italian":       "",
	"Rivals Quick Start Set": "",
	"Modern Horizons":        "",
}

func makeEditionEntry(set *mtgjson.Set, names ...string) EditionEntry {
	date, _ := time.Parse("2006-01-02", set.ReleaseDate)

	name := set.Name
	if len(names) > 0 && names[0] != "" {
		name = names[0]
	}
	special := false
	switch set.Code {
	case "H1R",
		"SCD":
		special = true
	}
	return EditionEntry{
		Name:    name,
		Code:    set.Code,
		Date:    date,
		Keyrune: strings.ToLower(set.KeyruneCode),
		Size:    len(set.Cards),
		FmtDate: set.ReleaseDate,
		Special: special,
		ShowFin: !set.IsNonFoilOnly && !set.IsFoilOnly,
		HasReg:  !set.IsFoilOnly,
		HasFoil: !set.IsNonFoilOnly,
	}
}

func getAllEditions() ([]string, map[string]EditionEntry) {
	sets := mtgmatcher.GetSets()

	sortedEditions := make([]string, 0, len(sets))
	listEditions := map[string]EditionEntry{}
	for _, set := range sets {
		sortedEditions = append(sortedEditions, set.Code)

		listEditions[set.Code] = makeEditionEntry(set)
	}

	sort.Slice(sortedEditions, func(i, j int) bool {
		return listEditions[sortedEditions[i]].Date.After(listEditions[sortedEditions[j]].Date)
	})

	return sortedEditions, listEditions
}

func getTreeEditions() ([]string, map[string][]EditionEntry) {
	sets := mtgmatcher.GetSets()

	sortedEditions := make([]string, 0, len(sets))
	listEditions := map[string][]EditionEntry{}
	for _, set := range sets {
		entry := makeEditionEntry(set)

		if set.ParentCode == "" {
			// Skip if it was already added from the other case
			_, found := listEditions[set.Code]
			if found {
				continue
			}
			// Create the head, list in the slice to be sorted
			listEditions[set.Code] = []EditionEntry{entry}
			sortedEditions = append(sortedEditions, set.Code)
		} else {
			// Find the very fist parent
			topParentCode := set.ParentCode
			for sets[topParentCode].ParentCode != "" {
				topParentCode = sets[topParentCode].ParentCode
			}

			// Check if the head of the tree is already present
			_, found := listEditions[topParentCode]
			if !found {
				// If not, create it
				headEntry := makeEditionEntry(sets[topParentCode])
				listEditions[topParentCode] = []EditionEntry{headEntry}
				sortedEditions = append(sortedEditions, topParentCode)
			}
			// Append the new entry
			listEditions[topParentCode] = append(listEditions[topParentCode], entry)
		}
	}

	// Sort main list by date
	sort.Slice(sortedEditions, func(i, j int) bool {
		// Sort by name in case date is the same
		if listEditions[sortedEditions[i]][0].Date == listEditions[sortedEditions[j]][0].Date {
			return listEditions[sortedEditions[i]][0].Name < listEditions[sortedEditions[j]][0].Name
		}
		return listEditions[sortedEditions[i]][0].Date.After(listEditions[sortedEditions[j]][0].Date)
	})

	// Sort sublists by date
	for _, key := range sortedEditions {
		sort.Slice(listEditions[key], func(i, j int) bool {
			// Keep the first element always first
			if j == 0 {
				return false
			}
			// Sort by name in case date is the same
			if listEditions[key][i].Date == listEditions[key][j].Date {
				return listEditions[key][i].Name < listEditions[key][j].Name
			}
			return listEditions[key][i].Date.After(listEditions[key][j].Date)
		})
	}

	return sortedEditions, listEditions
}

func getSealedEditions() ([]string, map[string][]EditionEntry) {
	sets := mtgmatcher.GetSets()

	sortedEditions := []string{}
	listEditions := map[string][]EditionEntry{}
	for _, set := range sets {
		if set.SealedProduct == nil {
			continue
		}

		_, found := sealedEditionSkips[set.Name]
		if found {
			continue
		}
		// Skip Judge promos as they don't have a real product associated, except for 2014
		if strings.HasPrefix(set.Name, "Judge Gift Cards") && !strings.HasSuffix(set.Name, "2014") {
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

		rename = editionRenames[set.Name]

		entry := makeEditionEntry(set, rename)
		listEditions[category] = append(listEditions[category], entry)
	}

	for key := range listEditions {
		sort.Slice(listEditions[key], func(i, j int) bool {
			return listEditions[key][i].Date.After(listEditions[key][j].Date)
		})
		sortedEditions = append(sortedEditions, key)
	}

	sort.Slice(sortedEditions, func(i, j int) bool {
		return listEditions[sortedEditions[i]][0].Date.After(listEditions[sortedEditions[j]][0].Date)
	})

	return sortedEditions, listEditions
}

type ReprintEntry struct {
	UUID    string
	SetCode string
	Date    time.Time
	Price   float64
}

const (
	YearsBeforeReprint  = 2
	MinimumReprintPrice = 3.0
)

// Produce a map of card : []ReprintEntry containing array reprints sorted by age
func getReprintsGlobal() ([]string, map[string][]ReprintEntry) {
	var tcgLow mtgban.InventoryRecord
	var tcgMarket mtgban.InventoryRecord
	for _, seller := range Sellers {
		if seller == nil {
			continue
		}
		if seller.Info().Shorthand == TCG_LOW {
			tcgLow, _ = seller.Inventory()
		}
		if seller.Info().Shorthand == TCG_MARKET {
			tcgMarket, _ = seller.Inventory()
		}
	}

	sets := mtgmatcher.GetSets()
	uuids := mtgmatcher.GetUUIDs()

	var names []string
	listReprints := map[string][]ReprintEntry{}

	dupes := map[string]bool{}
	for _, co := range uuids {
		set, found := sets[co.SetCode]
		if !found {
			continue
		}

		// Skip very old stuff
		switch set.Code {
		case "PVAN", "OARC", "PCEL", "PDCI",
			"LEG", "DRK", "ATQ", "ARN", "PTK",
			"OPCA", "OHOP",
			"FBB", "4BB", "DRKITA", "LEGITA", "RIN", "4EDALT", "BCHR":
			continue
		}

		// Skip strange stuff
		if co.IsReserved || co.Sealed || co.IsFunny || mtgmatcher.IsToken(co.Name) ||
			co.BorderColor == "gold" || co.BorderColor == "silver" ||
			co.HasPromoType(mtgjson.PromoTypePromoPack) ||
			co.HasPromoType(mtgjson.PromoTypePrerelease) {
			continue
		}

		// Ignore the foil printing of printed cards
		if co.Foil && len(co.Finishes) > 1 {
			continue
		}

		// Skip processed cards (using scryfallId to catch foil/nonfoil)
		if dupes[co.Identifiers["scryfallId"]] {
			continue
		}
		dupes[co.Identifiers["scryfallId"]] = true

		// Load the date for the card
		releaseDate := set.ReleaseDate
		if co.OriginalReleaseDate != "" {
			releaseDate = co.OriginalReleaseDate
		}
		printDate, err := time.Parse("2006-01-02", releaseDate)
		if err != nil {
			continue
		}

		var price float64
		entries, found := tcgLow[co.UUID]
		if !found {
			entries, found = tcgMarket[co.UUID]
		}
		if found {
			price = entries[0].Price
		}

		// Append to the results
		listReprints[co.Name] = append(listReprints[co.Name], ReprintEntry{
			UUID:  co.UUID,
			Price: price,
			Date:  printDate,
		})
	}

	// Filter results
	for name, reprints := range listReprints {
		var shouldSkip bool
		for i := range reprints {
			// Skip cards that are not old enough
			if time.Now().Sub(reprints[i].Date).Hours()/24/365 <= YearsBeforeReprint {
				shouldSkip = true
				break
			}

			// Skip cards that are too low or that don't have a price
			price := reprints[i].Price
			if price < MinimumReprintPrice {
				shouldSkip = true
				break
			}
		}

		if shouldSkip {
			delete(listReprints, name)
			continue
		}

		names = append(names, name)
		sort.Slice(reprints, func(i, j int) bool {
			return reprints[i].Date.After(reprints[j].Date)
		})
		listReprints[name] = reprints
	}

	return names, listReprints
}

var ProductKeys = []string{
	"TotalValueByTcgLow",
	"TotalValueByTcgDirect",
	"TotalValueByTcgLowMinusBulk",
	"TotalValueBuylist",
	"TotalValueDirectNet",
}

var ProductFoilKeys = []string{
	"TotalFoilValueByTcgLow",
	"TotalFoilValueByTcgDirect",
	"TotalFoilValueByTcgLowMinusBulk",
	"TotalFoilValueBuylist",
	"TotalFoilValueDirectNet",
}

var ProductTitles = []string{
	"by TCGLow",
	"by TCG Direct",
	"by TCGLow without Bulk",
	"by CK Buylist",
	"by TCG Direct (net)",
}

const (
	bulkPrice = 2.99
)

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

func bulkBuylist(co *mtgmatcher.CardObject) float64 {
	var price float64
	switch co.Rarity {
	case "mythic":
		price = 0.30
		if co.Foil {
			price = 0.25
		}
	case "rare":
		price = 0.08
		if co.Foil {
			price = 0.15
		}
	case "common", "uncommon":
		price = 5.0 / 1000
		if co.Foil {
			price = 0.02
		}
	default:
		if co.IsPromo {
			price = 0.05
		} else if mtgmatcher.IsBasicLand(co.Name) {
			price = 0.01
			if co.Foil {
				price = 0.10
			}
		}
	}
	return price
}

func runSealedAnalysis() {
	log.Println("Running set analysis")

	var tcgInventory mtgban.InventoryRecord
	var tcgDirect mtgban.InventoryRecord
	for _, seller := range Sellers {
		if seller == nil {
			continue
		}
		if seller.Info().Shorthand == TCG_LOW {
			tcgInventory, _ = seller.Inventory()
		}
		if seller.Info().Shorthand == TCG_DIRECT {
			tcgDirect, _ = seller.Inventory()
		}
	}

	var ckBuylist mtgban.BuylistRecord
	var directNetBuylist mtgban.BuylistRecord
	for _, vendor := range Vendors {
		if vendor == nil {
			continue
		}
		switch vendor.Info().Shorthand {
		case "CK":
			ckBuylist, _ = vendor.Buylist()
		case "TCGDirectNet":
			directNetBuylist, _ = vendor.Buylist()
		}
	}

	runRawSetValue(tcgInventory, tcgDirect, ckBuylist, directNetBuylist)
}

func runRawSetValue(tcgInventory, tcgDirect mtgban.InventoryRecord, ckBuylist, directNetBuylist mtgban.BuylistRecord) {
	inv := map[string]float64{}
	invFoil := map[string]float64{}
	invDirect := map[string]float64{}
	invDirectFoil := map[string]float64{}
	invNoBulk := map[string]float64{}
	invNoBulkFoil := map[string]float64{}
	bl := map[string]float64{}
	blFoil := map[string]float64{}
	blDirectNet := map[string]float64{}
	blDirectNetFoil := map[string]float64{}

	uuids := mtgmatcher.GetUUIDs()
	for uuid, co := range uuids {
		// Skip sets that are not well tracked upstream
		if co.SetCode == "PMEI" || co.BorderColor == "gold" {
			continue
		}

		// Determine whether to keep prices separated or combine them
		useFoil := co.Foil && !combineFinish(co.SetCode)

		var blPrice float64
		entriesBl, found := ckBuylist[uuid]
		if !found {
			blPrice = bulkBuylist(&co)
		} else {
			blPrice = entriesBl[0].BuyPrice
		}
		if useFoil {
			blFoil[co.SetCode] += blPrice
		} else {
			bl[co.SetCode] += blPrice
		}

		entriesInv, found := tcgInventory[uuid]
		if found {
			if useFoil {
				invFoil[co.SetCode] += entriesInv[0].Price
			} else {
				inv[co.SetCode] += entriesInv[0].Price
			}

			if entriesInv[0].Price > bulkPrice {
				if useFoil {
					invNoBulkFoil[co.SetCode] += entriesInv[0].Price
				} else {
					invNoBulk[co.SetCode] += entriesInv[0].Price
				}
			}
		}
		entriesInv, found = tcgDirect[uuid]
		if found {
			if useFoil {
				invDirectFoil[co.SetCode] += entriesInv[0].Price
			} else {
				invDirect[co.SetCode] += entriesInv[0].Price
			}
		}

		entriesBl, found = directNetBuylist[uuid]
		if found {
			if useFoil {
				blDirectNetFoil[co.SetCode] += entriesBl[0].BuyPrice
			} else {
				blDirectNet[co.SetCode] += entriesBl[0].BuyPrice
			}
		}
	}

	for i, records := range []map[string]float64{
		inv,
		invDirect,
		invNoBulk,
		bl,
		blDirectNet,
		invFoil,
		invDirectFoil,
		invNoBulkFoil,
		blFoil,
		blDirectNetFoil,
	} {
		record := mtgban.InventoryRecord{}
		for code, price := range records {
			record[code] = append(record[code], mtgban.InventoryEntry{
				Price: price,
			})
		}
		// Keep the two key sets separate
		key := ""
		if i >= len(ProductKeys) {
			key = ProductFoilKeys[i%len(ProductKeys)]
		} else {
			key = ProductKeys[i]
		}
		Infos[key] = record
	}
}

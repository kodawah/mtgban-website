package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
	"github.com/mtgban/go-mtgban/mtgmatcher/mtgjson"
	"golang.org/x/exp/slices"
)

type Sleeper struct {
	CardId string
	Level  int
}

const (
	SleeperSize = 7
	MaxSleepers = 34

	SleepersMinPrice = 3.0

	ErrNoSleepers = "No Sleepers Available"
)

var SleeperLetters = []string{
	"S", "A", "B", "C", "D", "E", "F",
}
var SleeperColors = []string{
	"#ff7f7f", "#ffbf7f", "#ffff7f", "#7fff7f", "#7fbfff", "#7f7fff", "#ff7fff",
}

func Sleepers(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Sleepers", sig)

	// Load the defaul blocklist (same as Search)
	blocklistRetail, blocklistBuylist := getDefaultBlocklists(sig)

	// Expand with any custom list if necessary
	if Config.SleepersBlockList != nil {
		blocklistRetail = append(blocklistRetail, Config.SleepersBlockList...)
		blocklistBuylist = append(blocklistBuylist, Config.SleepersBlockList...)
	}

	skipSellersOpt := readCookie(r, "SleepersSellersList")
	if skipSellersOpt != "" {
		blocklistRetail = append(blocklistRetail, strings.Split(skipSellersOpt, ",")...)
	}
	skipVendorsOpt := readCookie(r, "SleepersVendorsList")
	if skipVendorsOpt != "" {
		blocklistBuylist = append(blocklistBuylist, strings.Split(skipVendorsOpt, ",")...)
	}

	var skipEditions []string
	skipEditionsOpt := readCookie(r, "SleepersEditionList")
	if skipEditionsOpt != "" {
		skipEditions = strings.Split(skipEditionsOpt, ",")
	}

	var tiers map[string]int

	start := time.Now()

	page := r.FormValue("page")
	switch page {
	default:
		pageVars.Title = "Index"

		render(w, "sleep.html", pageVars)

		return
	case "options":
		pageVars.Title = "Options"

		for _, seller := range Sellers {
			if seller == nil ||
				seller.Info().CountryFlag != "" ||
				seller.Info().SealedMode ||
				seller.Info().MetadataOnly ||
				slices.Contains(blocklistRetail, seller.Info().Shorthand) {
				continue
			}

			pageVars.SellerKeys = append(pageVars.SellerKeys, seller.Info().Shorthand)
		}

		for _, vendor := range Vendors {
			if vendor == nil ||
				vendor.Info().CountryFlag != "" ||
				vendor.Info().SealedMode ||
				slices.Contains(blocklistBuylist, vendor.Info().Shorthand) {
				continue
			}

			pageVars.VendorKeys = append(pageVars.VendorKeys, vendor.Info().Shorthand)
		}

		pageVars.Editions = AllEditionsKeys
		pageVars.EditionsMap = AllEditionsMap

		render(w, "sleep.html", pageVars)

		return
	case "bulk":
		pageVars.Title = "Bulk me up"

		tiers = getBulks(skipEditions)

	case "reprint":
		pageVars.Title = "Long time no reprint"

		tiers = getReprints(skipEditions)
	case "mismatch":
		pageVars.Title = "Market Mismatch"

		tiers = getTiers(blocklistRetail, blocklistBuylist, skipEditions)
	}

	sleepers, err := sleepersLayout(tiers)
	if err != nil {
		pageVars.Title = "Errors have been made"
		pageVars.InfoMessage = ErrNoSleepers

		render(w, "sleep.html", pageVars)
		return
	}

	pageVars.Metadata = map[string]GenericCard{}
	for _, cardIds := range sleepers {
		for _, cardId := range cardIds {
			_, found := pageVars.Metadata[cardId]
			if !found {
				pageVars.Metadata[cardId] = uuid2card(cardId, true)
			}
		}
	}

	pageVars.Sleepers = sleepers
	pageVars.SleepersKeys = SleeperLetters
	pageVars.SleepersColors = SleeperColors

	// Log performance
	user := GetParamFromSig(sig, "UserEmail")
	msg := fmt.Sprintf("Sleepers call by %s with took %v", user, time.Since(start))
	UserNotify("Sleepers", msg)
	LogPages["Sleepers"].Println(msg)
	if DevMode {
		log.Println(msg)
	}

	if DevMode {
		start = time.Now()
	}
	render(w, "sleep.html", pageVars)
	if DevMode {
		log.Println("Sleepers render took", time.Since(start))
	}
}

func getBulks(skipEditions []string) map[string]int {
	var tcgSeller mtgban.Seller
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Shorthand == TCG_LOW {
			tcgSeller = seller
			break
		}
	}
	var ckVendor mtgban.Vendor
	for _, vendor := range Vendors {
		if vendor != nil && vendor.Info().Shorthand == "CK" {
			ckVendor = vendor
			break
		}
	}

	inv, err := tcgSeller.Inventory()
	if err != nil {
		return nil
	}
	bl, err := ckVendor.Buylist()
	if err != nil {
		return nil
	}

	tiers := map[string]int{}

	sets := mtgmatcher.GetSets()
	for _, set := range sets {
		if slices.Contains(skipEditions, set.Code) {
			continue
		}

		// Commander sets would pollute results too much
		switch set.Code {
		case "OPCA", "PLIST", "MB1", "30A":
			continue
		case "40K":
		default:
			if set.Type == "commander" {
				continue
			}
		}

		// Skip anything older than 5 years
		releaseDate, err := time.Parse("2006-01-02", set.ReleaseDate)
		if err != nil {
			continue
		}
		if time.Now().Sub(releaseDate).Hours()/24/365 > 5 {
			continue
		}

		count := 0
		cardPrices := map[string]float64{}
		var totalPrices float64
		for _, card := range set.Cards {
			uuid := mtgmatcher.Scryfall2UUID(card.Identifiers["scryfallId"])
			co, err := mtgmatcher.GetUUID(uuid)
			if err != nil {
				continue
			}
			if co.Foil || co.Etched || co.IsPromo ||
				co.HasPromoType(mtgjson.PromoTypeBoosterfun) ||
				co.HasPromoType(mtgjson.PromoTypePromoPack) {
				continue
			}

			// Only consider common and uncommon cards
			entries, found := inv[uuid]
			if !found {
				continue
			}
			if card.Rarity == "common" || card.Rarity == "uncommon" {
				count++
				price := entries[0].Price
				totalPrices += price
				cardPrices[uuid] = price
			}
		}
		if count == 0 {
			continue
		}

		averagePrice := totalPrices / float64(count)

		for uuid, price := range cardPrices {
			if price < averagePrice {
				continue
			}

			// Assign a value considering how big of a gap the minimum price has
			tiers[uuid] = int(price-averagePrice) + 1

			// Assign additional value if buylist has non-bulk worth
			var blPrice float64
			blEntries, found := bl[uuid]
			if found {
				blPrice = blEntries[0].BuyPrice
			}
			if blPrice > SleepersMinPrice {
				tiers[uuid] += 1
			}
		}
	}

	return tiers
}

func getReprints(skipEditions []string) map[string]int {
	tiers := map[string]int{}

	// Filter results
	for _, key := range ReprintsKeys {
		reprints, found := ReprintsMap[key]
		if !found {
			continue
		}

		var minPrice float64
		var uuid string
		latest := reprints[0].Date

		for _, reprint := range reprints {
			if slices.Contains(skipEditions, reprint.SetCode) {
				continue
			}
			if minPrice == 0 || minPrice > reprint.Price {
				minPrice = reprint.Price
				uuid = reprint.UUID
			}
		}
		// Sanity check
		if minPrice == 0 {
			continue
		}

		// Assign a custom value to the card
		// Use Seconds to give a heavier weight on older items and square of
		// price to let expensive cards have a bigger impact
		// Log just spreads the results more nicely on the tier system
		tiers[uuid] = int(math.Log(float64(time.Now().Sub(latest).Seconds()) * minPrice * minPrice))
	}

	return tiers
}

func getTiers(blocklistRetail, blocklistBuylist, skipEditions []string) map[string]int {
	tiers := map[string]int{}

	var tcgSeller mtgban.Seller
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Shorthand == TCG_LOW {
			tcgSeller = seller
			break
		}
	}

	opts := &mtgban.ArbitOpts{
		MinSpread: MinSpread,
		MinPrice:  SleepersMinPrice,
		Editions:  skipEditions,
	}

	for _, seller := range Sellers {
		if seller == nil {
			continue
		}

		if seller.Info().MetadataOnly {
			continue
		}
		if seller.Info().CountryFlag != "" {
			continue
		}
		if seller.Info().SealedMode {
			continue
		}

		// Skip any seller explicitly in blocklist
		if slices.Contains(blocklistRetail, seller.Info().Shorthand) {
			continue
		}

		for _, vendor := range Vendors {
			if vendor == nil {
				continue
			}
			if vendor.Info().Shorthand == seller.Info().Shorthand {
				continue
			}
			if vendor.Info().CountryFlag != "" {
				continue
			}

			// Skip any vendor explicitly in blocklist
			if slices.Contains(blocklistBuylist, vendor.Info().Shorthand) {
				continue
			}

			arbit, err := mtgban.Arbit(opts, vendor, seller)
			if err != nil {
				log.Println(err)
				continue
			}

			// Filter out entries that are invalid
			for i := range arbit {
				if math.Abs(arbit[i].BuylistEntry.PriceRatio) < MaxPriceRatio && arbit[i].Spread < MaxSpread && arbit[i].InventoryEntry.Conditions == "NM" {
					tiers[arbit[i].CardId]++
				}
			}
		}

		if tcgSeller != nil {
			mismatch, err := mtgban.Mismatch(opts, tcgSeller, seller)
			if err != nil {
				log.Println(err)
				continue
			}

			// Filter out entries that are invalid
			for i := range mismatch {
				if mismatch[i].InventoryEntry.Conditions == "NM" {
					tiers[mismatch[i].CardId]++
				}
			}
		}
	}

	return tiers
}

// Return a map of letter : []cardId from a map of cardId : amount
func sleepersLayout(tiers map[string]int) (map[string][]string, error) {
	results := []Sleeper{}
	for c := range tiers {
		if tiers[c] > 1 {
			results = append(results, Sleeper{
				CardId: c,
				Level:  tiers[c],
			})
		}
	}

	if len(results) == 0 {
		return nil, errors.New("empty results")
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Level > results[j].Level
	})

	maxrange := float64(SleeperSize - 1)
	minrange := float64(0)
	exp := float64(minrange - maxrange)
	max := float64(results[0].Level)
	min := float64(results[len(results)-1].Level)

	if DevMode {
		log.Println("Max value:", max)
		log.Println("Min value:", min)
	}

	// Avoid a division by 0
	if max == min {
		return nil, errors.New("invalid range")
	}

	sleepers := map[string][]string{}
	for _, res := range results {
		value := float64(res.Level)
		// Normalize between 0,1
		r := (value - min) / (max - min)
		// Scale to the size of the table
		level := int(math.Floor(r*exp) + maxrange)

		if DevMode {
			cc, _ := mtgmatcher.GetUUID(res.CardId)
			log.Println(level, res.Level, cc)
		}

		if level >= SleeperSize {
			break
		}

		letter := SleeperLetters[level]

		sleepers[letter] = append(sleepers[letter], res.CardId)
	}

	// Sort sleepers by price
	var tcgSeller mtgban.Seller
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Shorthand == TCG_LOW {
			tcgSeller = seller
			break
		}
	}
	inv, err := tcgSeller.Inventory()
	if err != nil {
		return nil, err
	}
	for _, letter := range SleeperLetters {
		sort.Slice(sleepers[letter], func(i, j int) bool {
			var priceI, priceJ float64
			entries, found := inv[sleepers[letter][i]]
			if found {
				priceI = entries[0].Price
			}
			entries, found = inv[sleepers[letter][j]]
			if found {
				priceJ = entries[0].Price
			}
			// Just to preserve order
			if priceI == priceJ {
				return sleepers[letter][i] < sleepers[letter][j]
			}
			return priceI > priceJ
		})

		// Truncate to avoid flooding the page
		if len(sleepers[letter]) > MaxSleepers {
			sleepers[letter] = sleepers[letter][:MaxSleepers]
		}
	}

	return sleepers, nil
}

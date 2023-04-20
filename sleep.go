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
)

type Sleeper struct {
	CardId string
	Level  int
}

const (
	SleeperSize = 7
	MaxSleepers = 34

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

	page := r.FormValue("page")
	if page == "options" {
		pageVars.Title = "Options"

		for _, seller := range Sellers {
			if seller == nil ||
				seller.Info().CountryFlag != "" ||
				seller.Info().SealedMode ||
				seller.Info().MetadataOnly ||
				SliceStringHas(blocklistRetail, seller.Info().Shorthand) {
				continue
			}

			pageVars.SellerKeys = append(pageVars.SellerKeys, seller.Info().Shorthand)
		}

		for _, vendor := range Vendors {
			if vendor == nil ||
				vendor.Info().CountryFlag != "" ||
				vendor.Info().SealedMode ||
				SliceStringHas(blocklistBuylist, vendor.Info().Shorthand) {
				continue
			}

			pageVars.VendorKeys = append(pageVars.VendorKeys, vendor.Info().Shorthand)
		}

		render(w, "sleep.html", pageVars)

		return
	}

	skipSellersOpt := readCookie(r, "SleepersSellersList")
	if skipSellersOpt != "" {
		blocklistRetail = append(blocklistRetail, strings.Split(skipSellersOpt, ",")...)
	}
	skipVendorsOpt := readCookie(r, "SleepersVendorsList")
	if skipVendorsOpt != "" {
		blocklistBuylist = append(blocklistBuylist, strings.Split(skipVendorsOpt, ",")...)
	}

	start := time.Now()

	tiers := getTiers(blocklistRetail, blocklistBuylist)
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

	pageVars.Title = "Sleeper cards"

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

func getTiers(blocklistRetail, blocklistBuylist []string) map[string]int {
	tiers := map[string]int{}

	var tcgSeller mtgban.Seller
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Shorthand == TCG_LOW {
			tcgSeller = seller
			break
		}
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
		if SliceStringHas(blocklistRetail, seller.Info().Shorthand) {
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
			if SliceStringHas(blocklistBuylist, vendor.Info().Shorthand) {
				continue
			}

			opts := &mtgban.ArbitOpts{
				MinSpread: MinSpread,
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
			mismatch, err := mtgban.Mismatch(nil, tcgSeller, seller)
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

		if len(sleepers[letter]) > MaxSleepers {
			continue
		}

		sleepers[letter] = append(sleepers[letter], res.CardId)
	}

	return sleepers, nil
}

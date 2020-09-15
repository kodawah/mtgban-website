package main

import (
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"

	"github.com/kodabb/go-mtgban/mtgban"
)

type Sleeper struct {
	CardId string
	Level  int
}

type SleeperEntry struct {
	Meta    []GenericCard
	Letter  string
	BGColor string
}

const (
	MaxSleepers = 34
)

var SleeperLetters = []string{
	"S", "A", "B", "C", "D", "E", "F",
}
var SleeperColors = []string{
	"#ff7f7f", "#ffbf7f", "#ffff7f", "#7fff7f", "#7fbfff", "#7f7fff", "#ff7fff",
}

func Sleepers(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	pageVars := genPageNav("Sleepers", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "sleep.html", pageVars)
		return
	}

	arbitParam, _ := GetParamFromSig(sig, "Sleepers")
	canSearch, _ := strconv.ParseBool(arbitParam)
	if SigCheck && !canSearch {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus
		pageVars.ShowPromo = true

		render(w, "sleep.html", pageVars)
		return
	}

	tiers := map[string]int{}

	var tcgSeller mtgban.Seller
	for _, seller := range Sellers {
		if seller.Info().Shorthand == "TCG Low" {
			tcgSeller = seller
			break
		}
	}

	for i, seller := range Sellers {
		if seller == nil {
			log.Println("nil seller at position", i)
			continue
		}

		if seller.Info().MetadataOnly {
			continue
		}

		for j, vendor := range Vendors {
			if vendor == nil {
				log.Println("nil vendor at position", j)
				continue
			}
			if vendor.Info().Name == seller.Info().Name {
				continue
			}

			if vendor.Info().Name == "TCG Player" {
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
			tmp := arbit[:0]
			for i := range arbit {
				if math.Abs(arbit[i].BuylistEntry.PriceRatio) < MaxPriceRatio && arbit[i].Spread < MaxSpread && arbit[i].InventoryEntry.Conditions == "NM" {
					tmp = append(tmp, arbit[i])
				}
			}
			arbit = tmp

			for _, arb := range arbit {
				tiers[arb.CardId]++
			}
		}

		if tcgSeller != nil {
			mismatch, err := mtgban.Mismatch(nil, tcgSeller, seller)
			if err != nil {
				log.Println(err)
				continue
			}

			// Filter out entries that are invalid
			tmp := mismatch[:0]
			for i := range mismatch {
				if mismatch[i].InventoryEntry.Conditions == "NM" {
					tmp = append(tmp, mismatch[i])
				}
			}
			mismatch = tmp

			for _, mis := range mismatch {
				tiers[mis.CardId]++
			}
		}
	}

	results := []Sleeper{}
	for c := range tiers {
		if tiers[c] > 1 {
			results = append(results, Sleeper{
				CardId: c,
				Level:  tiers[c],
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Level > results[j].Level
	})

	// Avoid accessing the first element if empty
	if len(results) == 0 {
		render(w, "sleep.html", pageVars)
		return
	}

	maxrange := float64(len(pageVars.Sleepers) - 1)
	minrange := float64(0)
	exp := float64(minrange - maxrange)
	max := float64(results[0].Level)
	min := float64(results[len(results)-1].Level)

	// Avoid a division by 0
	if max == min {
		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = ErrMsgDenied

		render(w, "sleep.html", pageVars)
		return
	}

	for i := range pageVars.Sleepers {
		pageVars.Sleepers[i].Meta = []GenericCard{}
		pageVars.Sleepers[i].Letter = SleeperLetters[i]
		pageVars.Sleepers[i].BGColor = SleeperColors[i]
	}

	for _, res := range results {
		value := float64(res.Level)
		// Normalize between 0,1
		r := (value - min) / (max - min)
		// Scale to the size of the table
		level := int(math.Floor(r*exp) + maxrange)

		if level >= len(pageVars.Sleepers) {
			break
		}

		if len(pageVars.Sleepers[level].Meta) > MaxSleepers {
			continue
		}

		pageVars.Sleepers[level].Meta = append(pageVars.Sleepers[level].Meta, uuid2card(res.CardId, true))
	}

	pageVars.Title = "Sleeper cards"

	render(w, "sleep.html", pageVars)
}

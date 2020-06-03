package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
)

func Arbit(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	pageVars := genPageNav("Arbitrage", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "arbit.html", pageVars)
		return
	}

	arbitParam, _ := GetParamFromSig(sig, "Arbit")
	canSearch, _ := strconv.ParseBool(arbitParam)
	if SigCheck && !canSearch {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus
		pageVars.ShowPromo = true

		render(w, "arbit.html", pageVars)
		return
	}
	enabled, _ := GetParamFromSig(sig, "Enabled")
	if enabled == "" && !SigCheck {
		enabled = "ALL"
	}
	if enabled == "ALL" {
		shorthands := []string{}
		for _, scraper := range BanClient.Scrapers() {
			shorthands = append(shorthands, scraper.Info().Shorthand)
		}
		enabled = strings.Join(shorthands, ",")
	} else if enabled == "DEFAULT" {
		enabled = DefaultSellers
	}

	r.ParseForm()

	var ok bool
	var source mtgban.Seller
	var useCredit bool
	var message string
	var sorting string

	for k, v := range r.Form {
		switch k {
		case "source":
			if !strings.Contains(enabled, v[0]) {
				log.Println("Unauthorized attempt with", v[0])
				message = "Unknown " + v[0] + " seller"
				break
			}
			scraper, err := BanClient.ScraperByName(v[0])
			if err != nil {
				message = "Unknown " + v[0] + " seller"
				break
			}
			source, ok = scraper.(mtgban.Seller)
			if !ok {
				message = "Unknown " + v[0] + " seller (vendor only?)"
				break
			}

		case "credit":
			switch v[0] {
			case "true":
				useCredit = true
			}

		case "sort":
			sorting = v[0]
		}
	}

	if message != "" {
		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = message

		render(w, "arbit.html", pageVars)
		return
	}

	for _, newSeller := range Sellers {
		if !strings.Contains(enabled, newSeller.Info().Shorthand) {
			continue
		}

		nav := NavElem{
			Name: newSeller.Info().Name,
			Link: "arbit?source=" + newSeller.Info().Shorthand,
		}
		if sig != "" {
			nav.Link += "&sig=" + sig
		}

		if source != nil && source.Info().Name == newSeller.Info().Name {
			nav.Active = true
			nav.Class = "selected"
		}
		pageVars.Nav = append(pageVars.Nav, nav)
	}

	if source == nil {
		pageVars.Title = "Arbitrage Opportunities"

		render(w, "arbit.html", pageVars)
		return
	}

	pageVars.SellerShort = source.Info().Shorthand
	pageVars.SellerFull = source.Info().Name
	pageVars.SellerUpdate = source.Info().InventoryTimestamp.Format(time.RFC3339)
	pageVars.CKPartner = CKPartner
	pageVars.TCGAffiliate = TCGConfig.Affiliate
	pageVars.UseCredit = useCredit

	pageVars.Arb = []Arbitrage{}
	pageVars.Images = map[mtgdb.Card]string{}

	for _, vendor := range Vendors {
		if vendor.Info().Name == source.Info().Name {
			continue
		}

		opts := &mtgban.ArbitOpts{
			MinSpread: 10,
		}
		if vendor.Info().Shorthand == "ABU" {
			opts.UseTrades = useCredit
		}

		arbit, err := mtgban.Arbit(opts, vendor, source)
		if err != nil {
			log.Println(err)
			continue
		}

		if len(arbit) == 0 {
			continue
		}

		for _, arb := range arbit {
			card := arb.Card
			code, _ := mtgdb.EditionName2Code(card.Edition)
			link := fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=small", strings.ToLower(code), card.Number)
			pageVars.Images[card] = link
		}

		switch sorting {
		case "sell_price":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].InventoryEntry.Price > arbit[j].InventoryEntry.Price
			})
		case "buy_price":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].BuylistEntry.BuyPrice > arbit[j].BuylistEntry.BuyPrice
			})
		case "trade_price":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].BuylistEntry.TradePrice > arbit[j].BuylistEntry.TradePrice
			})
		case "diff":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].Difference > arbit[j].Difference
			})
		default:
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].Spread > arbit[j].Spread
			})
		}

		for i := len(arbit) - 1; i >= 0; i-- {
			if arbit[i].Spread > 650 {
				log.Printf("Skipping impossible spread of %f", arbit[i].Spread)
				arbit = arbit[i:]
				break
			}
		}

		pageVars.Arb = append(pageVars.Arb, Arbitrage{
			Name:       vendor.Info().Name,
			LastUpdate: vendor.Info().BuylistTimestamp.Format(time.RFC3339),
			Arbit:      arbit,
			Len:        len(arbit),
		})
	}

	if len(pageVars.Arb) == 0 {
		pageVars.InfoMessage = "No arbitrage available!"
	}
	pageVars.Title = "Arbitrage from " + source.Info().Name

	render(w, "arbit.html", pageVars)
}

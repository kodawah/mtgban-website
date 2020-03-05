package main

import (
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

func Arbit(w http.ResponseWriter, r *http.Request) {
	pageVars := PageVars{
		Title: "BAN Arbitrage",
	}
	if DB == nil {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "arbit.html", pageVars)
		return
	}

	r.ParseForm()

	var ok bool
	var vendor mtgban.Vendor
	var seller mtgban.Seller
	var dumpCSV, dumpBL, useCredit bool
	var message string
	var sellerUpdate, vendorUpdate time.Time

	for k, v := range r.Form {
		switch k {
		case "vendor":
			scraper, err := BanClient.ScraperByName(v[0])
			if err != nil {
				log.Println(err)
				message = "Unknown " + v[0] + " vendor"
				break
			}
			vendor, ok = scraper.(mtgban.Vendor)
			if !ok {
				message = "Unknown " + v[0] + " vendor (seller only?)"
				break
			}

		case "seller":
			scraper, err := BanClient.ScraperByName(v[0])
			if err != nil {
				log.Println(err)
				message = "Unknown " + v[0] + " seller"
				break
			}
			seller, ok = scraper.(mtgban.Seller)
			if !ok {
				message = "Unknown " + v[0] + " seller (vendor only?)"
			}

		case "action":
			switch v[0] {
			case "csv":
				dumpCSV = true
			case "dlbl":
				dumpBL = true
			}

		case "credit":
			switch v[0] {
			case "true":
				useCredit = true
			}
		}
	}
	if message != "" {
		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = message

		render(w, "arbit.html", pageVars)
		return
	}

	if dumpBL {
		vendorFromSeller, ok := seller.(mtgban.Scraper).(mtgban.Vendor)
		if ok {
			mtgban.WriteBuylistToCSV(vendorFromSeller, w)
			return
		}

		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = "Vendor is not a seller"

		render(w, "arbit.html", pageVars)
		return
	}

	var sellerShort, sellerFull, vendorFull, vendorShort string
	if seller != nil {
		sellerShort = seller.Info().Shorthand
		sellerFull = seller.Info().Name
		sellerUpdate = seller.Info().InventoryTimestamp
	}
	if vendor != nil {
		vendorShort = vendor.Info().Shorthand
		vendorFull = vendor.Info().Name
		vendorUpdate = vendor.Info().BuylistTimestamp
	}

	pageVars.SellerShort = sellerShort
	pageVars.SellerFull = sellerFull
	pageVars.SellerUpdate = sellerUpdate.Format(time.RFC3339)
	pageVars.VendorShort = vendorShort
	pageVars.VendorFull = vendorFull
	pageVars.VendorUpdate = vendorUpdate.Format(time.RFC3339)
	pageVars.ErrorMessage = message
	pageVars.CKPartner = CKPartner
	pageVars.LastUpdate = LastUpdate.Format(time.RFC3339)
	pageVars.UseCredit = useCredit

	if vendor == nil {
		pageVars.Title = sellerFull + " Arbitrage"
		pageVars.VendorShort = sellerShort
		pageVars.VendorFull = sellerFull

		render(w, "arbit.html", pageVars)
		return
	}

	opts := &mtgban.ArbitOpts{
		MinSpread: 10,
		UseTrades: useCredit,
	}
	arbit, err := mtgban.Arbit(opts, vendor, seller)
	if err != nil {
		pageVars.Title = "Arbitrage Error"
		pageVars.ErrorMessage = err.Error()

		render(w, "arbit.html", pageVars)
		return
	}

	if len(arbit) == 0 {
		pageVars.InfoMessage = "No arbitrage found"
	}

	sort.Slice(arbit, func(i, j int) bool {
		return arbit[i].Spread > arbit[j].Spread
	})

	if dumpCSV {
		mtgban.WriteArbitrageToCSV(arbit, w)
		return
	}

	pageVars.Title = sellerFull + " arbitrage towards " + vendorFull
	pageVars.Arb = arbit

	render(w, "arbit.html", pageVars)
}

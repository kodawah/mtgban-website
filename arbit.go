package main

import (
	"net/http"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

func Arbit(w http.ResponseWriter, r *http.Request) {
	if DB == nil {
		pageVars := PageVars{
			Title:   "Great things are coming",
			Message: "Website is starting, please try again in a few minutes",
		}

		render(w, "arbit.html", pageVars)
		return
	}

	r.ParseForm()

	var vendor mtgban.Vendor
	var seller mtgban.Seller
	var dumpCSV, dumpBL bool
	var message string

	for k, v := range r.Form {
		switch k {
		case "vendor":
			switch v[0] {
			case "SZ":
				vendor = sz
			case "CFB":
				vendor = cfb
			case "ABU":
				vendor = abu
			case "MM":
				vendor = mm
			case "CK":
				vendor = ck
			default:
				message = "Unknown " + v[0] + " vendor"
			}

		case "seller":
			switch v[0] {
			case "SZ":
				seller = sz
			case "CFB":
				seller = cfb
			case "ABU":
				seller = abu
			case "MM":
				seller = mm
			case "CK":
				seller = ck
			default:
				message = "Unknown " + v[0] + " seller"
			}

		case "action":
			switch v[0] {
			case "csv":
				dumpCSV = true
			case "dlbl":
				dumpBL = true
			}
		}
	}
	if message != "" {
		pageVars := PageVars{
			Title:   "Errors have been made",
			Message: message,
		}

		render(w, "arbit.html", pageVars)
		return
	}

	if dumpBL {
		mtgban.WriteBuylistToCSV(seller.(mtgban.Scraper).(mtgban.Vendor), w)
		return
	}

	var vendorFull, vendorShort string
	sellerShort := seller.(mtgban.Scraper).Info().Shorthand
	sellerFull := seller.(mtgban.Scraper).Info().Name
	if vendor != nil {
		vendorShort = vendor.(mtgban.Scraper).Info().Shorthand
		vendorFull = vendor.(mtgban.Scraper).Info().Name
	}

	pageVars := PageVars{
		SellerShort: sellerShort,
		SellerFull:  sellerFull,
		VendorShort: vendorShort,
		VendorFull:  vendorFull,
		Message:     message,
		CKPartner:   CKPartner,
		LastUpdate:  LastUpdate.Format(time.RFC3339),
	}

	if vendor == nil {
		pageVars.Title = sellerFull + " Arbitrage"
		pageVars.VendorShort = sellerShort
		pageVars.VendorFull = sellerFull

		render(w, "arbit.html", pageVars)
		return
	}

	arbit, err := mtgban.Arbit(nil, vendor, seller)
	if err != nil {
		pageVars.Title = "Arbitrage Error"
		pageVars.Message = err.Error()

		render(w, "arbit.html", pageVars)
		return
	}

	if dumpCSV {
		mtgban.WriteArbitrageToCSV(arbit, w)
		return
	}

	pageVars.Title = sellerFull + " arbitrage towards " + vendorFull
	pageVars.Arb = arbit

	render(w, "arbit.html", pageVars)
}

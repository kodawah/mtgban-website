package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

func signHMACSHA1Base64(key []byte, data []byte) string {
	h := hmac.New(sha1.New, key)
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func Arbit(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("Signature")
	exp := r.FormValue("Expires")

	signature := ""
	if sig != "" && exp != "" {
		signature = "?Signature=" + url.QueryEscape(sig) + "&Expires=" + url.QueryEscape(exp)
	}

	pageVars := PageVars{
		Title:      "BAN Arbitrage",
		Signature:  sig,
		Expires:    exp,
		LastUpdate: LastUpdate.Format(time.RFC3339),
	}
	pageVars.Nav = make([]NavElem, len(DefaultNav))
	copy(pageVars.Nav, DefaultNav)

	mainNavIndex := 0
	for i := range pageVars.Nav {
		pageVars.Nav[i].Link += signature
		if pageVars.Nav[i].Name == "Arbitrage" {
			mainNavIndex = i
		}
	}
	pageVars.Nav[mainNavIndex].Active = true
	pageVars.Nav[mainNavIndex].Class = "active"

	if sig != "" && exp != "" {
		signature = "&Signature=" + url.QueryEscape(sig) + "&Expires=" + url.QueryEscape(exp)
	}

	data := fmt.Sprintf("%s%s%s", r.Method, exp, r.URL.Host)
	valid := signHMACSHA1Base64([]byte(os.Getenv("BAN_SECRET")), []byte(data))
	expires, err := strconv.ParseInt(exp, 10, 64)
	if !DevMode && (err != nil || valid != sig || expires < time.Now().Unix()) {
		pageVars.Title = "Unauthorized"
		pageVars.ErrorMessage = "Please double check your invitation link"

		render(w, "arbit.html", pageVars)
		return
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

	if seller == nil {
		for _, newSeller := range Sellers {
			pageVars.Nav = append(pageVars.Nav, NavElem{
				Name: newSeller.Info().Name,
				Link: "arbit?seller=" + newSeller.Info().Shorthand + signature,
			})
		}
	} else {
		pageVars.Nav[mainNavIndex].Active = false

		class := "active"
		if vendor != nil {
			class = "selected"
		}
		baseLink := "arbit?seller=" + seller.Info().Shorthand
		pageVars.Nav = append(pageVars.Nav, NavElem{
			Active: true,
			Class:  class,
			Name:   seller.Info().Name,
			Link:   baseLink + signature,
		})

		for _, targetVendor := range Vendors {
			if seller.(mtgban.Scraper) == targetVendor.(mtgban.Scraper) {
				continue
			}
			pageVars.Nav = append(pageVars.Nav, NavElem{
				Active: vendor == targetVendor,
				Class:  "active",
				Name:   targetVendor.Info().Name,
				Link:   baseLink + "&vendor=" + targetVendor.Info().Shorthand + signature,
			})
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

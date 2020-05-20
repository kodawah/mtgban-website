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

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "arbit.html", pageVars)
		return
	}

	r.ParseForm()

	var ok bool
	var source mtgban.Seller
	var useCredit bool
	var message string

	for k, v := range r.Form {
		switch k {
		case "source":
			scraper, err := BanClient.ScraperByName(v[0])
			if err != nil {
				log.Println(err)
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
		}
	}

	if message != "" {
		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = message

		render(w, "arbit.html", pageVars)
		return
	}

	for _, newSeller := range Sellers {
		nav := NavElem{
			Name: newSeller.Info().Name,
			Link: "arbit?source=" + newSeller.Info().Shorthand + signature,
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
	pageVars.UseCredit = useCredit

	pageVars.Arb = []Arbitrage{}

	for _, vendor := range Vendors {
		if vendor.(mtgban.Scraper) == source.(mtgban.Scraper) {
			continue
		}

		opts := &mtgban.ArbitOpts{
			MinSpread: 10,
		}
		if vendor.Info().Shorthand == "ABU" {
			opts.UseTrades = useCredit
		}

		log.Println("Comparing", source.Info().Shorthand, "->", vendor.Info().Shorthand)
		arbit, err := mtgban.Arbit(opts, vendor, source)
		if err != nil {
			log.Println(err)
			continue
		}

		log.Println(len(arbit), "offers")
		if len(arbit) == 0 {
			continue
		}

		sort.Slice(arbit, func(i, j int) bool {
			return arbit[i].Spread > arbit[j].Spread
		})

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

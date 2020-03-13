package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

func Search(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("Signature")
	exp := r.FormValue("Expires")

	signature := ""
	if sig != "" && exp != "" {
		signature = "?Signature=" + url.QueryEscape(sig) + "&Expires=" + url.QueryEscape(exp)
	}

	pageVars := PageVars{
		Title:      "BAN Search",
		Signature:  sig,
		Expires:    exp,
		LastUpdate: LastUpdate.Format(time.RFC3339),
	}
	pageVars.Nav = make([]NavElem, len(DefaultNav))
	copy(pageVars.Nav, DefaultNav)

	mainNavIndex := 0
	for i := range pageVars.Nav {
		pageVars.Nav[i].Link += signature
		if pageVars.Nav[i].Name == "Search" {
			mainNavIndex = i
		}
	}
	pageVars.Nav[mainNavIndex].Active = true
	pageVars.Nav[mainNavIndex].Class = "active"

	data := fmt.Sprintf("%s%s%s", r.Method, exp, r.URL.Host)
	valid := signHMACSHA1Base64([]byte(os.Getenv("BAN_SECRET")), []byte(data))
	expires, err := strconv.ParseInt(exp, 10, 64)
	if !DevMode && (err != nil || valid != sig || expires < time.Now().Unix()) {
		pageVars.Title = "Unauthorized"
		pageVars.ErrorMessage = "Please double check your invitation link"

		render(w, "search.html", pageVars)
		return
	}

	if DB == nil {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "search.html", pageVars)
		return
	}

	query := r.FormValue("q")

	if query != "" {
		pageVars.SearchQuery = query
		pageVars.FoundSellers = map[mtgban.Card][]mtgban.CombineEntry{}
		pageVars.FoundVendors = map[mtgban.Card][]mtgban.CombineEntry{}

		query = Norm.Normalize(query)

		for card, entries := range GlobalInventory.Entries {
			if strings.HasPrefix(Norm.Normalize(card.Name), query) {
				for _, entry := range entries {
					_, found := pageVars.FoundSellers[card]
					if !found {
						pageVars.FoundSellers[card] = []mtgban.CombineEntry{}
					}
					if entry.Price > 0 {
						pageVars.FoundSellers[card] = append(pageVars.FoundSellers[card], entry)
					}
				}
			}
		}

		for card, entries := range GlobalBuylist.Entries {
			if strings.HasPrefix(Norm.Normalize(card.Name), query) {
				for _, entry := range entries {
					_, found := pageVars.FoundVendors[card]
					if !found {
						pageVars.FoundVendors[card] = []mtgban.CombineEntry{}
					}
					if entry.Price > 0 {
						pageVars.FoundVendors[card] = append(pageVars.FoundVendors[card], entry)
					}
				}
			}
		}

		if len(pageVars.FoundSellers) == 0 && len(pageVars.FoundVendors) == 0 {
			pageVars.InfoMessage = "No results found"
		}
	}

	render(w, "search.html", pageVars)
	runtime.GC()
}

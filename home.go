package main

import (
	"net/http"
	"net/url"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("Signature")
	exp := r.FormValue("Expires")

	signature := ""
	if sig != "" && exp != "" {
		signature = "?Signature=" + url.QueryEscape(sig) + "&Expires=" + url.QueryEscape(exp)
	}

	pageVars := PageVars{
		Title:     "Welcome to BAN",
		Signature: url.QueryEscape(sig),
		Expires:   url.QueryEscape(exp),
		Nav: []NavElem{
			NavElem{
				Active: true,
				Class:  "active",
				Name:   "Home",
				Link:   "/" + signature,
			},
			NavElem{
				Name: "Arbitrage",
				Link: "arbit" + signature,
			},
		},
	}
	render(w, "home.html", pageVars)
}

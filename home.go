package main

import (
	"net/http"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	pageVars := PageVars{
		Title:     "Welcome to BAN",
		Nav: []NavElem{
			NavElem{
				Active: true,
				Class:  "active",
				Name:   "Home",
				Link:   "/?",
			},
			NavElem{
				Name: "Arbitrage",
				Link: "arbit?",
			},
		},
	}
	render(w, "home.html", pageVars)
}

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
	}
	pageVars.Nav = make([]NavElem, len(DefaultNav))
	copy(pageVars.Nav, DefaultNav)

	pageVars.Nav[0].Active = true
	pageVars.Nav[0].Class = "active"
	for i := range pageVars.Nav {
		pageVars.Nav[i].Link += signature
	}

	render(w, "home.html", pageVars)
}

package main

import (
	"net/http"
)

func Explore(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Explore", sig)

	site := r.FormValue("site")
	q := r.FormValue("q")

	pageVars.SearchQuery = q

	if site == "" {
		render(w, "explore.html", pageVars)
		return
	}

	// TODO separate by types
	enabled, _ := GetParamFromSig(sig, "ExpEnabled")
	switch enabled {
	case "ALL":
	case "FULL":
	case "MOST":
	case "ENTRY":
	case "DEMO":
	default:
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus

		render(w, "explore.html", pageVars)
		return
	}

	var uri string
	err := ExploreDB.QueryRow("SELECT uri FROM demo WHERE id = ?", site).Scan(&uri)
	if err != nil {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = err.Error()

		render(w, "explore.html", pageVars)
		return
	}

	http.Redirect(w, r, uri+q, http.StatusFound)
}

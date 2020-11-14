package main

import (
	"net/http"
	"strconv"
)

func Explore(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Explore", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "explore.html", pageVars)
		return
	}

	exploreParam, _ := GetParamFromSig(sig, "Explore")
	canExplore, _ := strconv.ParseBool(exploreParam)
	if SigCheck && !canExplore {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus
		pageVars.ShowPromo = true

		render(w, "explore.html", pageVars)
		return
	}

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

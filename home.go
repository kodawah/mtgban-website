package main

import (
	"net/http"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")
	errmsg := r.FormValue("errmsg")

	pageVars := genPageNav("Home", sig)
	pageVars.PatreonId = PatreonClientId
	pageVars.PatreonURL = PatreonHost
	pageVars.PatreonPartnerId = PatreonPartnerId

	switch errmsg {
	case "TokenNotFound":
		pageVars.ErrorMessage = "There was a problem authenticating you with Patreon."
	case "UserNotFound", "TierNotFound":
		pageVars.ErrorMessage = ErrMsg
	}

	render(w, "home.html", pageVars)
}

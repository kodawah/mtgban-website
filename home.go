package main

import (
	"net/http"
	"time"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")
	errmsg := r.FormValue("errmsg")

	pageVars := genPageNav("Home", sig)

	switch errmsg {
	case "TokenNotFound":
		pageVars.ErrorMessage = "There was a problem authenticating you with Patreon."
	case "UserNotFound", "TierNotFound":
		pageVars.ErrorMessage = ErrMsg
	case "logout":
		cookie := http.Cookie{
			Name:    "MTGBAN",
			Domain:  getBaseURL(r),
			Path:    "/",
			Expires: time.Now(),
		}
		http.SetCookie(w, &cookie)
	}

	render(w, "home.html", pageVars)
}

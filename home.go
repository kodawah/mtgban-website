package main

import (
	"net/http"
	"strings"
	"time"
)

// Handler for / renders the home.html page
func Home(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)
	errmsg := r.FormValue("errmsg")
	message := ""

	switch errmsg {
	case "TokenNotFound":
		message = "There was a problem authenticating you with Patreon."
	case "UserNotFound", "TierNotFound":
		message = ErrMsg
	case "logout":
		domain := "mtgban.com"
		if strings.Contains(getBaseURL(r), "localhost") {
			domain = "localhost"
		}

		// Invalidate the current cookie
		cookie := http.Cookie{
			Name:    "MTGBAN",
			Domain:  domain,
			Path:    "/",
			Expires: time.Now(),
		}
		http.SetCookie(w, &cookie)

		// Delete signature
		sig = ""
	}

	pageVars := genPageNav("Home", sig)
	pageVars.ErrorMessage = message

	render(w, "home.html", pageVars)
}

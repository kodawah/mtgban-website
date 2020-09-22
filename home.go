package main

import (
	"net/http"
	"strings"
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
		domain := "mtgban.com"
		if strings.Contains(getBaseURL(r), "localhost") {
			domain = "localhost"
		}

		cookie := http.Cookie{
			Name:    "MTGBAN",
			Domain:  domain,
			Path:    "/",
			Expires: time.Now(),
		}
		http.SetCookie(w, &cookie)
	}

	render(w, "home.html", pageVars)
}

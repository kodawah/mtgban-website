package main

import (
	"net/http"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	pageVars := genPageNav("Home", sig)

	render(w, "home.html", pageVars)
}

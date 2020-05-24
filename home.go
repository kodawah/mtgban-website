package main

import (
	"net/http"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("Signature")
	exp := r.FormValue("Expires")

	pageVars := genPageNav("Home", sig, exp)

	render(w, "home.html", pageVars)
}

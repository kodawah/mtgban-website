package main

import (
	"net/http"
)

//handler for / renders the home.html
func Home(w http.ResponseWriter, r *http.Request) {
	pageVars := PageVars{
		Title: "Welcome to BAN",
	}
	render(w, "home.html", pageVars)
}

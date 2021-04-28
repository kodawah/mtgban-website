package main

import (
	"net/http"
	"net/url"
	"strings"
)

const UTM_BOT = "banbot"

func Redirect(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/go/")
	fields := strings.Split(path, "/")

	if len(fields) == 3 {
		kind := fields[0]
		store := fields[1]
		hash := fields[2]

		if kind == "r" || kind == "i" {
			for _, seller := range Sellers {
				if seller != nil && seller.Info().Shorthand == store {
					inv, err := seller.Inventory()
					if err != nil {
						http.NotFound(w, r)
						return
					}
					entries := inv[hash]
					for _, entry := range entries {
						link := entry.URL
						// Change the utm default query param to improve tracking
						if strings.HasPrefix(store, "TCG") {
							u, err := url.Parse(link)
							if err != nil {
								break
							}
							v := u.Query()
							v.Set("utm_medium", UTM_BOT)
							u.RawQuery = v.Encode()
							link = u.String()
						}
						http.Redirect(w, r, link, http.StatusFound)
						return
					}
				}
			}
		} else if kind == "b" {
			for _, vendor := range Vendors {
				if vendor != nil && vendor.Info().Shorthand == store {
					bl, err := vendor.Buylist()
					if err != nil {
						http.NotFound(w, r)
						return
					}
					entries := bl[hash]
					for _, entry := range entries {
						http.Redirect(w, r, entry.URL, http.StatusFound)
						return
					}
				}
			}
		}
	}

	http.NotFound(w, r)
}

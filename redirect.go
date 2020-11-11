package main

import (
	"net/http"
	"strings"
)

func Redirect(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/go/")
	fields := strings.Split(path, "/")

	if len(fields) == 3 {
		kind := fields[0]
		store := fields[1]
		hash := fields[2]

		if kind == "r" {
			for _, seller := range Sellers {
				if seller != nil && seller.Info().Shorthand == store {
					inv, err := seller.Inventory()
					if err != nil {
						http.NotFound(w, r)
						return
					}
					entries := inv[hash]
					for _, entry := range entries {
						http.Redirect(w, r, entry.URL, http.StatusFound)
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

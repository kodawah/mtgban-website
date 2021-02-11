package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	mtgjsonURL = "https://mtgjson.com/api/v5/AllPrintings.json"
)

func Admin(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Admin", sig)

	msg := r.FormValue("msg")
	if msg != "" {
		pageVars.InfoMessage = msg
	}

	refresh := r.FormValue("refresh")
	if refresh != "" {
		key, found := ScraperMap[refresh]
		if !found {
			pageVars.InfoMessage = refresh + " not found"
		}
		if key != "" {
			_, found := ScraperOptions[key]
			if !found {
				pageVars.InfoMessage = key + " not found"
			} else {
				// Strip the request parameter to avoid accidental repeats
				// and to give a chance to table to update
				r.URL.RawQuery = ""
				if ScraperOptions[key].Busy {
					v := url.Values{
						"msg": {key + " is already being refreshed"},
					}
					r.URL.RawQuery = v.Encode()
				} else if len(ScraperOptions[key].Keepers) > 0 {
					go reloadMarket(key)
				} else {
					go reloadSingle(key)
				}

				http.Redirect(w, r, r.URL.String(), http.StatusFound)
				return
			}
		}
	}
	reboot := r.FormValue("reboot")
	switch reboot {
	case "mtgstocks":
		v := url.Values{
			"msg": {"Refreshing MTGStocks in the background..."},
		}
		r.URL.RawQuery = v.Encode()

		go loadInfos()
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
		return

	case "mtgjson":
		v := url.Values{
			"msg": {"Reloading MTGJSON in the background..."},
		}
		r.URL.RawQuery = v.Encode()

		go func() {
			log.Println("Retrieving the latest version of mtgjson")
			resp, err := cleanhttp.DefaultClient().Get(mtgjsonURL)
			if err != nil {
				log.Println(err)
				return
			}
			defer resp.Body.Close()

			log.Println("Installing the new mtgjson version")
			err = mtgmatcher.LoadDatastore(resp.Body)
			if err != nil {
				log.Println(err)
				return
			}
			log.Println("New mtgjson is ready")
		}()
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
		return

	case "server":
		v := url.Values{
			"msg": {"Restarting the server..."},
		}
		r.URL.RawQuery = v.Encode()

		// Let the system restart the server
		go func() {
			time.Sleep(5 * time.Second)
			log.Println("Admin requested server restart")
			os.Exit(0)
		}()
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
		return
	}

	pageVars.Headers = []string{
		"Name", "Short", "Last Update", "Entries", "Status",
	}
	for i := range Sellers {
		if Sellers[i] == nil {
			row := []string{
				fmt.Sprintf("Error at Seller %d", i), "", "", "", "",
			}
			pageVars.Table = append(pageVars.Table, row)
			continue
		}

		scraperOptions := ScraperOptions[ScraperMap[Sellers[i].Info().Shorthand]]

		lastUpdate := Sellers[i].Info().InventoryTimestamp.Format(time.Stamp)

		inv, _ := Sellers[i].Inventory()

		status := "✅"
		if scraperOptions.Busy {
			status = "🔶"
		} else if len(inv) == 0 {
			status = "🔴"
		}

		row := []string{
			Sellers[i].Info().Name,
			Sellers[i].Info().Shorthand,
			lastUpdate,
			fmt.Sprint(len(inv)),
			status,
		}

		pageVars.Table = append(pageVars.Table, row)
	}

	for i := range Vendors {
		if Vendors[i] == nil {
			row := []string{
				fmt.Sprintf("Error at Vendor %d", i), "", "", "", "",
			}
			pageVars.Table = append(pageVars.Table, row)
			continue
		}

		scraperOptions := ScraperOptions[ScraperMap[Vendors[i].Info().Shorthand]]

		lastUpdate := Vendors[i].Info().BuylistTimestamp.Format(time.Stamp)

		bl, _ := Vendors[i].Buylist()

		status := "✅"
		if scraperOptions.Busy {
			status = "🔶"
		} else if len(bl) == 0 {
			status = "🔴"
		}

		row := []string{
			Vendors[i].Info().Name,
			Vendors[i].Info().Shorthand,
			lastUpdate,
			fmt.Sprint(len(bl)),
			status,
		}

		pageVars.OtherTable = append(pageVars.OtherTable, row)
	}

	render(w, "admin.html", pageVars)
}

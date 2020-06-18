package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
	"github.com/kodabb/go-mtgban/mtgjson"
)

func Search(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	pageVars := genPageNav("Search", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

		render(w, "search.html", pageVars)
		return
	}

	searchParam, _ := GetParamFromSig(sig, "Search")
	canSearch, _ := strconv.ParseBool(searchParam)
	if SigCheck && !canSearch {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus
		pageVars.ShowPromo = true

		render(w, "search.html", pageVars)
		return
	}

	query := r.FormValue("q")

	if query != "" {
		pageVars.SearchQuery = query
		pageVars.CondKeys = []string{"TCG", "NM", "SP", "MP", "HP", "PO"}
		pageVars.FoundSellers = map[mtgdb.Card]map[string][]mtgban.CombineEntry{}
		pageVars.FoundVendors = map[mtgdb.Card][]mtgban.CombineEntry{}
		pageVars.Images = map[mtgdb.Card]string{}

		filterEdition := ""
		filterCondition := ""
		filterFoil := ""
		for _, tag := range []string{"s:", "c:", "f:"} {
			if strings.Contains(query, tag) {
				fields := strings.Fields(query)
				for _, field := range fields {
					if strings.HasPrefix(field, tag) {
						query = strings.TrimPrefix(query, field)
						query = strings.TrimSuffix(query, field)
						query = strings.TrimSpace(query)

						code := strings.TrimPrefix(field, tag)
						switch tag {
						case "s:":
							filterEdition, _ = mtgdb.EditionCode2Name(code)
							break
						case "c:":
							filterCondition = code
							break
						case "f:":
							filterFoil = code
							if filterFoil == "yes" || filterFoil == "y" {
								filterFoil = "true"
							} else if filterFoil == "no" || filterFoil == "n" {
								filterFoil = "false"
							}
							break
						}
					}
				}
			}
		}

		cmpFunc := mtgjson.NormPrefix
		if strings.HasPrefix(query, "\"") && strings.HasSuffix(query, "\"") {
			cmpFunc = mtgjson.NormEquals
			query = strings.TrimPrefix(query, "\"")
			query = strings.TrimSuffix(query, "\"")
			query = strings.TrimSpace(query)
		} else if strings.HasPrefix(query, "*") && strings.HasSuffix(query, "*") {
			cmpFunc = mtgjson.NormContains
			query = strings.TrimPrefix(query, "*")
			query = strings.TrimSuffix(query, "*")
			query = strings.TrimSpace(query)
		}

		for _, seller := range Sellers {
			inventory, err := seller.Inventory()
			if err != nil {
				log.Println(err)
				continue
			}
			for card, entries := range inventory {
				if cmpFunc(card.Name, query) {
					if filterEdition != "" && filterEdition != card.Edition {
						continue
					}
					if filterFoil != "" {
						foilStatus, err := strconv.ParseBool(filterFoil)
						if err == nil {
							if foilStatus && !card.Foil {
								continue
							} else if !foilStatus && card.Foil {
								continue
							}
						}
					}

					if pageVars.Images[card] == "" {
						code, _ := mtgdb.EditionName2Code(card.Edition)
						link := fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=normal", strings.ToLower(code), card.Number)
						pageVars.Images[card] = link
					}

					for _, entry := range entries {
						if filterCondition != "" && filterCondition != entry.Conditions {
							continue
						}
						if entry.Price == 0 {
							continue
						}

						_, found := pageVars.FoundSellers[card]
						if !found {
							pageVars.FoundSellers[card] = map[string][]mtgban.CombineEntry{}
						}

						conditions := entry.Conditions
						if seller.Info().Name == "TCG Low" || seller.Info().Name == "TCG Direct Low" {
							conditions = "TCG"
						}
						_, found = pageVars.FoundSellers[card][conditions]
						if !found {
							pageVars.FoundSellers[card][conditions] = []mtgban.CombineEntry{}
						}

						res := mtgban.CombineEntry{
							ScraperName: seller.Info().Name,
							Price:       entry.Price,
							Quantity:    entry.Quantity,
							URL:         entry.URL,
						}
						pageVars.FoundSellers[card][conditions] = append(pageVars.FoundSellers[card][conditions], res)
					}
				}
			}
		}

		for _, vendor := range Vendors {
			buylist, err := vendor.Buylist()
			if err != nil {
				log.Println(err)
				continue
			}
			for card, entry := range buylist {
				if filterEdition != "" && filterEdition != card.Edition {
					continue
				}
				if filterFoil != "" {
					foilStatus, err := strconv.ParseBool(filterFoil)
					if err == nil {
						if foilStatus && !card.Foil {
							continue
						} else if !foilStatus && card.Foil {
							continue
						}
					}
				}

				if pageVars.Images[card] == "" {
					code, _ := mtgdb.EditionName2Code(card.Edition)
					link := fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=normal", strings.ToLower(code), card.Number)
					pageVars.Images[card] = link
				}

				if cmpFunc(card.Name, query) {
					_, found := pageVars.FoundVendors[card]
					if !found {
						pageVars.FoundVendors[card] = []mtgban.CombineEntry{}
					}
					res := mtgban.CombineEntry{
						ScraperName: vendor.Info().Name,
						Price:       entry.BuyPrice,
						Ratio:       entry.PriceRatio,
						Quantity:    entry.Quantity,
						URL:         entry.URL,
					}
					pageVars.FoundVendors[card] = append(pageVars.FoundVendors[card], res)
				}
			}
		}

		if len(pageVars.FoundSellers) == 0 && len(pageVars.FoundVendors) == 0 {
			pageVars.InfoMessage = "No results found"
		}
	}

	render(w, "search.html", pageVars)
}

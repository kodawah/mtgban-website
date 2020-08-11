package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
	"github.com/kodabb/go-mtgban/mtgjson"
)

const (
	MaxSearchResults = 64
	TooManyMessage   = "More results available, try adjusting your filters"
	NoResultsMessage = "No results found"
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

	// Query is not null, let's get processing
	if query != "" {
		log.Println(query)

		// Keep track of what was searched
		pageVars.SearchQuery = query
		// Setup conditions keys, all etnries, and images
		pageVars.CondKeys = []string{"TCG", "NM", "SP", "MP", "HP", "PO"}
		pageVars.FoundSellers = map[mtgdb.Card]map[string][]mtgban.CombineEntry{}
		pageVars.FoundVendors = map[mtgdb.Card][]mtgban.CombineEntry{}
		pageVars.Metadata = map[mtgdb.Card]CardMeta{}

		// Set which comparison function to use depending on the search syntax
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

		// Filter out any element from the search syntax
		filterEdition := ""
		filterCondition := ""
		filterFoil := ""
		for _, tag := range []string{"s:", "c:", "f:", "sm:"} {
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
						case "sm:":
							switch code {
							case "exact":
								cmpFunc = mtgjson.NormEquals
							case "prefix":
								cmpFunc = mtgjson.NormPrefix
							case "any":
								cmpFunc = mtgjson.NormContains
							}
							break
						}
					}
				}
			}
		}

		// Handle split cards
		if strings.Contains(query, " // ") {
			s := strings.Split(query, " // ")
			query = s[0]
		}

		// Search sellers
		for i, seller := range Sellers {
			if seller == nil {
				log.Println("nil seller at position", i)
				continue
			}

			// Get inventory
			inventory, err := seller.Inventory()
			if err != nil {
				log.Println(err)
				continue
			}

			// Loop through cards
			for card, entries := range inventory {
				// Run the comparison function set above
				if cmpFunc(card.Name, query) {
					// Skip cards that are not of the desired set
					if filterEdition != "" && filterEdition != card.Edition {
						continue
					}
					// Skip cards that are not as desired foil
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

					// Loop thorugh available conditions
					for _, entry := range entries {
						// Load up image links
						_, found := pageVars.Metadata[card]
						if !found {
							link, err := ScryfallImageURL(card, false)
							if err != nil {
								log.Println(err)
							}
							html, title, err := KeyruneCodes(card)
							if err != nil {
								log.Println(err)
							}
							pageVars.Metadata[card] = CardMeta{
								ImageURL:     link,
								KeyruneHTML:  html,
								KeyruneTitle: title,
							}
						}

						// Skip cards that have not the desired condition
						if filterCondition != "" && filterCondition != entry.Conditions {
							continue
						}

						// No price no dice
						if entry.Price == 0 {
							continue
						}

						// Check if card already has any entry
						_, found = pageVars.FoundSellers[card]
						if !found {
							// Skip when you have too many results
							if len(pageVars.FoundSellers) > MaxSearchResults {
								pageVars.InfoMessage = TooManyMessage
								continue
							}
							pageVars.FoundSellers[card] = map[string][]mtgban.CombineEntry{}
						}

						// Set conditions - handle the special TCG one that appears
						// at the top of the results
						conditions := entry.Conditions
						if seller.Info().Name == "TCG Low" || seller.Info().Name == "TCG Direct Low" {
							conditions = "TCG"
						}
						// Check if the current entry has any condition
						_, found = pageVars.FoundSellers[card][conditions]
						if !found {
							pageVars.FoundSellers[card][conditions] = []mtgban.CombineEntry{}
						}

						// Prepare all the deets
						res := mtgban.CombineEntry{
							ScraperName: seller.Info().Name,
							Price:       entry.Price,
							Quantity:    entry.Quantity,
							URL:         entry.URL,
						}
						if seller.Info().CountryFlag != "" {
							res.ScraperName += " " + seller.Info().CountryFlag
						}

						// Touchdown
						pageVars.FoundSellers[card][conditions] = append(pageVars.FoundSellers[card][conditions], res)
					}
				}
			}
		}

		// Really same as above
		for i, vendor := range Vendors {
			if vendor == nil {
				log.Println("nil vendor at position", i)
				continue
			}

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

				if cmpFunc(card.Name, query) {
					_, found := pageVars.Metadata[card]
					if !found {
						link, err := ScryfallImageURL(card, false)
						if err != nil {
							log.Println(err)
						}
						html, title, err := KeyruneCodes(card)
						if err != nil {
							log.Println(err)
						}
						pageVars.Metadata[card] = CardMeta{
							ImageURL:     link,
							KeyruneHTML:  html,
							KeyruneTitle: title,
						}
					}

					_, found = pageVars.FoundVendors[card]
					if !found {
						if len(pageVars.FoundVendors) > MaxSearchResults {
							pageVars.InfoMessage = TooManyMessage
							continue
						}
						pageVars.FoundVendors[card] = []mtgban.CombineEntry{}
					}
					res := mtgban.CombineEntry{
						ScraperName: vendor.Info().Name,
						Price:       entry.BuyPrice,
						Ratio:       entry.PriceRatio,
						Quantity:    entry.Quantity,
						URL:         entry.URL,
					}
					if vendor.Info().CountryFlag != "" {
						res.ScraperName += " " + vendor.Info().CountryFlag
					}
					pageVars.FoundVendors[card] = append(pageVars.FoundVendors[card], res)
				}
			}
		}

		if len(pageVars.FoundSellers) == 0 && len(pageVars.FoundVendors) == 0 {
			pageVars.InfoMessage = NoResultsMessage
		}
	}

	render(w, "search.html", pageVars)
}

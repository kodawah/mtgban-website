package main

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgmatcher"
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

	blocklist, _ := GetParamFromSig(sig, "SearchDisabled")
	if blocklist == "NONE" && !SigCheck {
		blocklist = ""
	}
	if blocklist == "DEFAULT" {
		blocklist = strings.Join(Config.SearchBlockList, ",")
	}

	query := r.FormValue("q")
	bestSorting, _ := strconv.ParseBool(r.FormValue("b"))

	// Query is not null, let's get processing
	if query == "" {
		render(w, "search.html", pageVars)
		return
	}
	log.Println(query)

	// Keep track of what was searched
	pageVars.SearchQuery = query
	// Setup conditions keys, all etnries, and images
	pageVars.CondKeys = []string{"INDEX", "NM", "SP", "MP", "HP", "PO"}
	pageVars.FoundSellers = map[string]map[string][]mtgban.CombineEntry{}
	pageVars.FoundVendors = map[string][]mtgban.CombineEntry{}
	pageVars.Metadata = map[string]GenericCard{}

	// Set which comparison function to use depending on the search syntax
	cmpFunc := mtgmatcher.Equals

	// Filter out any element from the search syntax
	filterEdition := ""
	filterCondition := ""
	filterFoil := ""
	filterNumber := ""
	for _, tag := range []string{"s:", "c:", "f:", "sm:", "cn:"} {
		if strings.Contains(query, tag) {
			fields := strings.Fields(query)
			for _, field := range fields {
				if strings.HasPrefix(field, tag) {
					query = strings.Replace(query, field, "", 1)
					query = strings.TrimSpace(query)

					code := strings.TrimPrefix(field, tag)
					switch tag {
					case "s:":
						filterEdition = strings.ToUpper(code)
						break
					case "c:":
						filterCondition = code
						break
					case "cn:":
						filterNumber = code
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
							cmpFunc = mtgmatcher.Equals
						case "prefix":
							cmpFunc = mtgmatcher.HasPrefix
						case "any":
							cmpFunc = mtgmatcher.Contains
						}
						break
					}
				}
			}
		}
	}

	// Search sellers
	for i, seller := range Sellers {
		if seller == nil {
			log.Println("nil seller at position", i)
			continue
		}

		// Skip any seller explicitly in blocklist
		if strings.Contains(blocklist, seller.Info().Shorthand) {
			continue
		}

		// Get inventory
		inventory, err := seller.Inventory()
		if err != nil {
			log.Println(err)
			continue
		}

		// Loop through cards
		for cardId, entries := range inventory {
			co, err := mtgmatcher.GetUUID(cardId)
			if err != nil {
				continue
			}

			// Run the comparison function set above
			if cmpFunc(co.Card.Name, query) {
				// Skip cards that are not of the desired set
				if filterEdition != "" && filterEdition != co.SetCode {
					continue
				}
				// Skip cards that are not of the desired collector number
				if filterNumber != "" && filterNumber != co.Card.Number {
					continue
				}
				// Skip cards that are not as desired foil
				if filterFoil != "" {
					foilStatus, err := strconv.ParseBool(filterFoil)
					if err == nil {
						if foilStatus && !co.Foil {
							continue
						} else if !foilStatus && co.Foil {
							continue
						}
					}
				}

				// Loop thorugh available conditions
				for _, entry := range entries {
					// Load up image links
					_, found := pageVars.Metadata[cardId]
					if !found {
						pageVars.Metadata[cardId] = uuid2card(cardId, false)
					}

					if pageVars.Metadata[cardId].Reserved {
						pageVars.HasReserved = true
					}
					if pageVars.Metadata[cardId].Stocks {
						pageVars.HasStocks = true
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
					_, found = pageVars.FoundSellers[cardId]
					if !found {
						// Skip when you have too many results
						if len(pageVars.FoundSellers) > MaxSearchResults {
							pageVars.InfoMessage = TooManyMessage
							continue
						}
						pageVars.FoundSellers[cardId] = map[string][]mtgban.CombineEntry{}
					}

					// Set conditions - handle the special TCG one that appears
					// at the top of the results
					conditions := entry.Conditions
					if seller.Info().Name == "TCG Low" || seller.Info().Name == "TCG Direct Low" {
						conditions = "INDEX"
					}
					// Check if the current entry has any condition
					_, found = pageVars.FoundSellers[cardId][conditions]
					if !found {
						pageVars.FoundSellers[cardId][conditions] = []mtgban.CombineEntry{}
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
					pageVars.FoundSellers[cardId][conditions] = append(pageVars.FoundSellers[cardId][conditions], res)
				}
			}
		}
	}

	sortedKeysSeller := make([]string, 0, len(pageVars.FoundSellers))
	for cardId := range pageVars.FoundSellers {
		sortedKeysSeller = append(sortedKeysSeller, cardId)
	}

	sort.Slice(sortedKeysSeller, func(i, j int) bool {
		return sortSets(sortedKeysSeller[i], sortedKeysSeller[j])
	})

	if bestSorting {
		for cardId := range pageVars.FoundSellers {
			for cond := range pageVars.FoundSellers[cardId] {
				sort.Slice(pageVars.FoundSellers[cardId][cond], func(i, j int) bool {
					return pageVars.FoundSellers[cardId][cond][i].Price < pageVars.FoundSellers[cardId][cond][j].Price
				})
			}
		}
	}

	// Really same as above
	for i, vendor := range Vendors {
		if vendor == nil {
			log.Println("nil vendor at position", i)
			continue
		}

		if strings.Contains(blocklist, vendor.Info().Shorthand) {
			continue
		}

		buylist, err := vendor.Buylist()
		if err != nil {
			log.Println(err)
			continue
		}
		for cardId, entry := range buylist {
			co, err := mtgmatcher.GetUUID(cardId)
			if err != nil {
				continue
			}

			if filterEdition != "" && filterEdition != co.SetCode {
				continue
			}
			if filterNumber != "" && filterNumber != co.Card.Number {
				continue
			}
			if filterFoil != "" {
				foilStatus, err := strconv.ParseBool(filterFoil)
				if err == nil {
					if foilStatus && !co.Foil {
						continue
					} else if !foilStatus && co.Foil {
						continue
					}
				}
			}

			if cmpFunc(co.Card.Name, query) {
				_, found := pageVars.Metadata[cardId]
				if !found {
					pageVars.Metadata[cardId] = uuid2card(cardId, false)
				}

				if pageVars.Metadata[cardId].Reserved {
					pageVars.HasReserved = true
				}
				if pageVars.Metadata[cardId].Stocks {
					pageVars.HasStocks = true
				}

				_, found = pageVars.FoundVendors[cardId]
				if !found {
					if len(pageVars.FoundVendors) > MaxSearchResults {
						pageVars.InfoMessage = TooManyMessage
						continue
					}
					pageVars.FoundVendors[cardId] = []mtgban.CombineEntry{}
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
				pageVars.FoundVendors[cardId] = append(pageVars.FoundVendors[cardId], res)
			}
		}
	}

	sortedKeysVendor := make([]string, 0, len(pageVars.FoundVendors))
	for cardId := range pageVars.FoundVendors {
		sortedKeysVendor = append(sortedKeysVendor, cardId)
	}

	sort.Slice(sortedKeysVendor, func(i, j int) bool {
		return sortSets(sortedKeysVendor[i], sortedKeysVendor[j])
	})

	if bestSorting {
		for cardId := range pageVars.FoundVendors {
			sort.Slice(pageVars.FoundVendors[cardId], func(i, j int) bool {
				return pageVars.FoundVendors[cardId][i].Price > pageVars.FoundVendors[cardId][j].Price
			})
		}
	}

	if len(pageVars.FoundSellers) == 0 && len(pageVars.FoundVendors) == 0 {
		pageVars.InfoMessage = NoResultsMessage
	}

	pageVars.SellerKeys = sortedKeysSeller
	pageVars.VendorKeys = sortedKeysVendor

	render(w, "search.html", pageVars)
}

func sortSets(uuidI, uuidJ string) bool {
	set, err := mtgmatcher.GetSetUUID(uuidI)
	if err != nil {
		return false
	}
	setDateI, err := time.Parse("2006-01-02", set.ReleaseDate)
	if err != nil {
		return false
	}
	editionI := set.Name

	set, err = mtgmatcher.GetSetUUID(uuidJ)
	if err != nil {
		return false
	}
	setDateJ, err := time.Parse("2006-01-02", set.ReleaseDate)
	if err != nil {
		return false
	}
	editionJ := set.Name

	// If the two sets have the same release date, let's dig more
	if setDateI.Equal(setDateJ) {
		// If they are part of the same edition, check for their collector number
		// taking their foiling into consideration
		if editionI == editionJ {
			cI, errI := mtgmatcher.GetUUID(uuidI)
			cJ, errJ := mtgmatcher.GetUUID(uuidJ)
			// This should be impossible
			if errI != nil || errJ != nil {
				return false
			}

			// If their number is the same, check for foiling status
			if cI.Card.Number == cJ.Card.Number {
				if cI.Foil == true && cJ.Foil == false {
					return false
				} else if cI.Foil == false && cJ.Foil == true {
					return true
				}
			}

			// If both are foil or both are non-foil, check their number
			cInum, errI := strconv.Atoi(cI.Card.Number)
			cJnum, errJ := strconv.Atoi(cJ.Card.Number)
			if errI == nil && errJ == nil {
				return cInum < cJnum
			}
			// If either one is not a number (due to extra letters) just
			// do a normal string comparison
			return cI.Card.Number < cJ.Card.Number

			// For the special case of set promos, always keeps them after
		} else if editionJ == editionI+" Promos" {
			return true
		}
	}

	return setDateI.After(setDateJ)
}

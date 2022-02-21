package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	MaxSearchQueryLen = 200
	MaxSearchResults  = 64
	TooLongMessage    = "Your query planeswalked away, try a shorter one"
	TooManyMessage    = "More results available, try adjusting your filters"
	NoResultsMessage  = "No results found"
)

type SearchEntry struct {
	ScraperName string
	Shorthand   string
	Price       float64
	Credit      float64
	Ratio       float64
	Quantity    int
	URL         string
	NoQuantity  bool
	BundleIcon  string

	Country string

	IndexCombined bool
	Secondary     float64
}

var AllConditions = []string{"INDEX", "NM", "SP", "MP", "HP", "PO"}
var AllNormalConditions = []string{"NM", "SP", "MP", "HP", "PO"}

func Search(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Search", sig)

	blocklistRetail, blocklistBuylist := getDefaultBlocklists(sig)

	query := r.FormValue("q")

	pageVars.IsSets = r.URL.Path == "/sets"

	pageVars.Nav = insertNavBar("Search", pageVars.Nav, []NavElem{
		NavElem{
			Name:   "Sets",
			Short:  "📦",
			Link:   "/sets",
			Active: pageVars.IsSets,
			Class:  "selected",
		},
	})

	canSealed, _ := strconv.ParseBool(GetParamFromSig(sig, "SearchSealed"))
	canSealed = canSealed || (DevMode && !SigCheck)

	pageVars.IsSealed = r.URL.Path == "/sealed"

	if canSealed {
		pageVars.Nav = insertNavBar("Sets", pageVars.Nav, []NavElem{
			NavElem{
				Name:   "Sealed",
				Short:  "🧱",
				Link:   "/sealed",
				Active: pageVars.IsSealed,
				Class:  "selected",
			},
		})
	}

	if len(query) > MaxSearchQueryLen {
		pageVars.ErrorMessage = TooLongMessage

		render(w, "search.html", pageVars)
		return
	}

	chartId := r.FormValue("chart")
	// Check if query is a valid ID
	co, err := mtgmatcher.GetUUID(chartId)
	if err != nil {
		chartId = ""
	} else {
		// Override the query when chart is requested
		query = chartId
	}

	// If query is empty there is nothing to do
	if query == "" {
		// Hijack sealed list
		if pageVars.IsSealed {
			pageVars.EditionSort = SealedEditionsSorted
			pageVars.EditionList = SealedEditionsList
			render(w, "product.html", pageVars)
			return
		} else if pageVars.IsSets {
			pageVars.EditionSort = TreeEditionsKeys
			pageVars.EditionList = TreeEditionsMap
			pageVars.TotalSets = TotalSets
			pageVars.TotalCards = TotalCards
			pageVars.TotalUnique = TotalUnique

			sortOpt := r.FormValue("sort")

			if sortOpt == "name" {
				namedSort := make([]string, len(TreeEditionsKeys))
				copy(namedSort, TreeEditionsKeys)
				sort.Slice(namedSort, func(i, j int) bool {
					return TreeEditionsMap[namedSort[i]][0].Name < TreeEditionsMap[namedSort[j]][0].Name
				})
				pageVars.EditionSort = namedSort
			} else if sortOpt == "size" {
				sizeSort := make([]string, len(TreeEditionsKeys))
				copy(sizeSort, TreeEditionsKeys)
				sort.Slice(sizeSort, func(i, j int) bool {
					if TreeEditionsMap[sizeSort[i]][0].Size == TreeEditionsMap[sizeSort[j]][0].Size {
						return TreeEditionsMap[sizeSort[i]][0].Name < TreeEditionsMap[sizeSort[j]][0].Name
					}
					return TreeEditionsMap[sizeSort[i]][0].Size > TreeEditionsMap[sizeSort[j]][0].Size
				})
				pageVars.EditionSort = sizeSort
			}

			render(w, "editions.html", pageVars)
			return
		}

		render(w, "search.html", pageVars)
		return
	}

	start := time.Now()

	// Keep track of what was searched
	pageVars.SearchQuery = query
	pageVars.SearchBest = readSetFlag(w, r, "b", "MTGBANSearchPref")
	pageVars.CondKeys = AllConditions
	pageVars.Metadata = map[string]GenericCard{}

	config := parseSearchOptionsNG(query, blocklistRetail, blocklistBuylist)
	if pageVars.IsSealed {
		config.SearchMode = "sealed"
	}

	foundSellers, foundVendors := searchParallelNG(config)

	cleanQuery := config.CleanQuery
	canShowAll := (len(config.CardFilters) != 0 || len(config.UUIDs) != 0)

	// Only used in hashing searches, fill in data with what is available
	if config.FullQuery != "" {
		pageVars.SearchQuery = config.FullQuery
	}

	skipEmptyRetail := config.SkipEmptyRetail
	skipEmptyBuylist := config.SkipEmptyBuylist

	// Early exit if there no matches are found
	if len(foundSellers) == 0 && len(foundVendors) == 0 {
		pageVars.InfoMessage = NoResultsMessage
		render(w, "search.html", pageVars)
		return
	}

	// Allow displaying the "search all" link only when something
	// was searched and no options were specified for it
	pageVars.CanShowAll = cleanQuery != "" && canShowAll
	pageVars.CleanSearchQuery = cleanQuery

	// Make a cardId arrays so that they can be sorted later
	// Assume the same number of keys are found, will be reallocated if needed
	allKeys := make([]string, 0, len(foundSellers))

	// Append keys to the main array
	for cardId := range foundSellers {
		// Skip if skipEmptyBuylist and nothing was found in buylist
		if skipEmptyBuylist && len(foundVendors[cardId]) == 0 {
			continue
		}
		// Skip if skipEmptyRetail and only INDEX entries were found
		if skipEmptyRetail && len(foundSellers[cardId]) == 1 && len(foundSellers[cardId]["INDEX"]) != 0 {
			continue
		}
		// Always append the card to the main list
		allKeys = append(allKeys, cardId)
	}
	for cardId := range foundVendors {
		// Skip if skipEmptyRetail and nothing was found in retail
		if skipEmptyRetail && len(foundSellers[cardId]) == 0 {
			continue
		}
		// Append the card if it was not already added
		_, found := foundSellers[cardId]
		if !found {
			allKeys = append(allKeys, cardId)
		}
	}

	// Sort keys according to the sortSets() function, chronologically
	sort.Slice(allKeys, func(i, j int) bool {
		return sortSets(allKeys[i], allKeys[j])
	})

	// If results can't fit in one page, chunk response and enable pagination
	if len(allKeys) > MaxSearchResults {
		pageVars.TotalIndex = len(allKeys)/MaxSearchResults + 1

		// Parse the requested input page
		pageIndex, _ := strconv.Atoi(r.FormValue("p"))
		if pageIndex <= 1 {
			pageIndex = 1
		} else if pageIndex > pageVars.TotalIndex {
			pageIndex = pageVars.TotalIndex
		}

		// Assign the current page index to enable pagination
		pageVars.CurrentIndex = pageIndex

		// Initialize previous and next pagination links
		if pageVars.CurrentIndex > 0 {
			pageVars.PrevIndex = pageVars.CurrentIndex - 1
		}
		if pageVars.CurrentIndex < pageVars.TotalIndex {
			pageVars.NextIndex = pageVars.CurrentIndex + 1
		}

		// Chop results where needed
		head := MaxSearchResults * (pageIndex - 1)
		tail := MaxSearchResults * pageIndex
		if tail > len(allKeys) {
			tail = len(allKeys)
		}
		allKeys = allKeys[head:tail]
	}

	// Load up image links and other metadata
	for _, cardId := range allKeys {
		_, found := pageVars.Metadata[cardId]
		if !found {
			pageVars.Metadata[cardId] = uuid2card(cardId, false, true)
		}
		if pageVars.Metadata[cardId].Reserved {
			pageVars.HasReserved = true
		}
		if pageVars.Metadata[cardId].Stocks {
			pageVars.HasStocks = true
		}
		if pageVars.Metadata[cardId].SypList {
			pageVars.HasSypList = true
		}
	}

	// Optionally sort according to price
	if pageVars.SearchBest {
		for _, cardId := range allKeys {
			for cond := range foundSellers[cardId] {
				// These entries are special, do not sort them
				if cond == "INDEX" {
					continue
				}
				sort.Slice(foundSellers[cardId][cond], func(i, j int) bool {
					return foundSellers[cardId][cond][i].Price < foundSellers[cardId][cond][j].Price
				})
			}
			_, found := foundVendors[cardId]
			if found {
				sort.Slice(foundVendors[cardId], func(i, j int) bool {
					return foundVendors[cardId][i].Price > foundVendors[cardId][j].Price
				})
			}
		}
	}

	// Readjust array of INDEX entires
	for _, cardId := range allKeys {
		_, found := foundSellers[cardId]
		if !found {
			continue
		}
		indexArray := foundSellers[cardId]["INDEX"]
		tmp := indexArray[:0]
		mkmIndex := -1
		tcgIndex := -1

		// Iterate on array, always passthrough, except for specific entries
		for i := range indexArray {
			switch indexArray[i].ScraperName {
			case MKM_LOW:
				// Save reference to the array
				tmp = append(tmp, indexArray[i])
				mkmIndex = len(tmp) - 1
			case MKM_TREND:
				// If the reference is found, add a secondary price
				// otherwise just leave it as is
				if mkmIndex >= 0 {
					tmp[mkmIndex].Secondary = indexArray[i].Price
					tmp[mkmIndex].ScraperName = "MKM (Low / Trend)"
					tmp[mkmIndex].IndexCombined = true
				} else {
					tmp = append(tmp, indexArray[i])
				}
			case TCG_LOW:
				// Save reference to the array
				tmp = append(tmp, indexArray[i])
				tcgIndex = len(tmp) - 1
			case TCG_MARKET:
				// If the reference is found, add a secondary price
				// otherwise just leave it as is
				if tcgIndex >= 0 {
					tmp[tcgIndex].Secondary = indexArray[i].Price
					tmp[tcgIndex].ScraperName = "TCG (Low / Market)"
					tmp[tcgIndex].IndexCombined = true
				} else {
					tmp = append(tmp, indexArray[i])
				}
			case TCG_DIRECT_LOW:
				// Skip this one for search results
				continue
			default:
				tmp = append(tmp, indexArray[i])
			}
		}

		foundSellers[cardId]["INDEX"] = tmp
	}

	pageVars.FoundSellers = foundSellers
	pageVars.FoundVendors = foundVendors
	pageVars.AllKeys = allKeys

	// CHART ALL THE THINGS
	if chartId != "" {
		// Rebuild the search query by faking a uuid lookup
		cfg := parseSearchOptionsNG(chartId, nil, nil)
		pageVars.SearchQuery = cfg.FullQuery

		// Retrieve data
		labels, err := getDateAxisValues(chartId)
		if err != nil {
			pageVars.InfoMessage = "No chart data available"
		} else {
			pageVars.AxisLabels = labels
			pageVars.ChartID = chartId

			for _, config := range enabledDatasets {
				if co.Sealed && !config.HasSealed {
					continue
				}
				dataset, err := getDataset(chartId, labels, config)
				if err != nil {
					log.Println(err)
					continue
				}
				pageVars.Datasets = append(pageVars.Datasets, dataset)
			}
		}

		altId, err := mtgmatcher.Match(&mtgmatcher.Card{
			Id:   chartId,
			Foil: !co.Foil,
		})
		if err == nil && altId != chartId {
			pageVars.Alternative = altId
		}

		altId, err = mtgmatcher.Match(&mtgmatcher.Card{
			Id:        chartId,
			Variation: "Etched",
		})
		if err == nil && altId != chartId {
			pageVars.AltEtchedId = altId
		}

		pageVars.StocksURL = pageVars.Metadata[chartId].StocksURL
	} else {
		// Display tracking for non-chart requests
		var source string
		utm := r.FormValue("utm_source")
		if utm == "banbot" {
			id := r.FormValue("utm_affiliate")
			source = fmt.Sprintf("banbot (%s)", id)
		} else if utm == "autocard" {
			source = "autocard anywhere"
		} else {
			u, err := url.Parse(r.Referer())
			if err != nil {
				log.Println(err)
				source = "n/a"
			} else {
				if strings.Contains(u.Host, "mtgban") {
					source = u.Path
				} else {
					// Avoid automatic URL expansion in Discord
					source = fmt.Sprintf("<%s>", u.String())
				}
			}
		}
		user := GetParamFromSig(sig, "UserEmail")
		msg := fmt.Sprintf("[%s] from %s by %s (took %v)", query, source, user, time.Since(start))
		UserNotify("search", msg)
		LogPages["Search"].Println(msg)
		if DevMode {
			log.Println(msg)
		}
	}

	if DevMode {
		start = time.Now()
	}
	render(w, "search.html", pageVars)
	if DevMode {
		log.Println("render took", time.Since(start))
	}
}

func searchSellersNG(cardIds []string, config SearchConfig) (foundSellers map[string]map[string][]SearchEntry) {
	// Allocate memory
	foundSellers = map[string]map[string][]SearchEntry{}

	storeFilters := config.StoreFilters
	priceFilters := config.PriceFilters
	entryFilters := config.EntryFilters

	// Search sellers
	for _, seller := range Sellers {
		if shouldSkipStoreNG(seller, storeFilters) {
			continue
		}

		// Get inventory
		inventory, err := seller.Inventory()
		if err != nil {
			continue
		}

		for _, cardId := range cardIds {
			entries, found := inventory[cardId]
			if !found {
				continue
			}

			// Loop thorugh available conditions
			for _, entry := range entries {
				// Skip cards that have not the desired condition
				if !seller.Info().MetadataOnly && shouldSkipEntryNG(entry, entryFilters) {
					continue
				}

				// Skip cards that don't match desired pricing
				if shouldSkipPriceNG(cardId, entry, priceFilters) {
					continue
				}

				// Check if card already has any entry
				_, found := foundSellers[cardId]
				if !found {
					foundSellers[cardId] = map[string][]SearchEntry{}
				}

				// Set conditions - handle the special TCG one that appears
				// at the top of the results
				conditions := entry.Conditions
				if seller.Info().MetadataOnly {
					conditions = "INDEX"
				}

				// Only add Poor prices if there are no NM and SP entries
				if conditions == "PO" && len(foundSellers[cardId]["NM"]) != 0 && len(foundSellers[cardId]["SP"]) != 0 {
					continue
				}

				// Check if the current entry has any condition
				_, found = foundSellers[cardId][conditions]
				if !found {
					foundSellers[cardId][conditions] = []SearchEntry{}
				}

				icon := ""
				name := seller.Info().Name
				switch name {
				case TCG_DIRECT:
					icon = "img/misc/direct.png"
				case CT_ZERO:
					icon = "img/misc/zero.png"
				}

				// Prepare all the deets
				res := SearchEntry{
					ScraperName: name,
					Shorthand:   seller.Info().Shorthand,
					Price:       entry.Price,
					Quantity:    entry.Quantity,
					URL:         entry.URL,
					NoQuantity:  seller.Info().NoQuantityInventory || seller.Info().MetadataOnly,
					BundleIcon:  icon,
					Country:     Country2flag[seller.Info().CountryFlag],
				}

				// Touchdown
				foundSellers[cardId][conditions] = append(foundSellers[cardId][conditions], res)
			}
		}
	}

	return
}

func searchVendorsNG(cardIds []string, config SearchConfig) (foundVendors map[string][]SearchEntry) {
	foundVendors = map[string][]SearchEntry{}

	storeFilters := config.StoreFilters
	priceFilters := config.PriceFilters

	for _, vendor := range Vendors {
		if shouldSkipStoreNG(vendor, storeFilters) {
			continue
		}

		buylist, err := vendor.Buylist()
		if err != nil {
			continue
		}

		for _, cardId := range cardIds {
			blEntries, found := buylist[cardId]
			if !found {
				continue
			}

			// Look up the NM printing
			nmIndex := 0
			if vendor.Info().MultiCondBuylist {
				for nmIndex = range blEntries {
					if blEntries[nmIndex].Conditions == "NM" {
						break
					}
				}
			}
			entry := blEntries[nmIndex]

			if shouldSkipPriceNG(cardId, entry, priceFilters) {
				continue
			}

			_, found = foundVendors[cardId]
			if !found {
				foundVendors[cardId] = []SearchEntry{}
			}
			name := vendor.Info().Name
			if name == "TCG Player Market" {
				name = "TCG Trade-In"
			}
			res := SearchEntry{
				ScraperName: name,
				Shorthand:   vendor.Info().Shorthand,
				Price:       entry.BuyPrice,
				Credit:      entry.TradePrice,
				Ratio:       entry.PriceRatio,
				Quantity:    entry.Quantity,
				URL:         entry.URL,
				Country:     Country2flag[vendor.Info().CountryFlag],
			}
			foundVendors[cardId] = append(foundVendors[cardId], res)
		}
	}

	return
}

func searchAndFilter(config SearchConfig) ([]string, error) {
	query := config.CleanQuery
	filters := config.CardFilters

	var uuids []string
	var err error
	switch config.SearchMode {
	case "exact":
		uuids, err = mtgmatcher.SearchEquals(query)
	case "any":
		uuids, err = mtgmatcher.SearchContains(query)
	case "prefix":
		uuids, err = mtgmatcher.SearchHasPrefix(query)
	case "hashing":
		uuids = config.UUIDs
	case "sealed":
		uuids, err = mtgmatcher.SearchSealedEquals(query)
		if err != nil {
			uuids, err = mtgmatcher.SearchSealedContains(query)
		}
	default:
		uuids, err = mtgmatcher.SearchEquals(query)
		if err != nil {
			uuids, err = mtgmatcher.SearchHasPrefix(query)
		}
	}
	if err != nil {
		return nil, err
	}

	var selectedUUIDs []string
	for _, uuid := range uuids {
		if shouldSkipCardNG(uuid, filters) {
			continue
		}
		selectedUUIDs = append(selectedUUIDs, uuid)
	}
	return selectedUUIDs, nil
}

func searchParallelNG(config SearchConfig, flags ...bool) (foundSellers map[string]map[string][]SearchEntry, foundVendors map[string][]SearchEntry) {
	selectedUUIDs, err := searchAndFilter(config)
	if err != nil {
		return nil, nil
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		if !config.SkipRetail {
			foundSellers = searchSellersNG(selectedUUIDs, config)
		}
		wg.Done()
	}()
	go func() {
		if !config.SkipBuylist {
			foundVendors = searchVendorsNG(selectedUUIDs, config)
		}
		wg.Done()
	}()

	wg.Wait()

	return
}

type SortingData struct {
	co          *mtgmatcher.CardObject
	releaseDate time.Time
	parentCode  string
}

func getSortingData(uuid string) (*SortingData, error) {
	co, err := mtgmatcher.GetUUID(uuid)
	if err != nil {
		return nil, err
	}
	set, err := mtgmatcher.GetSet(co.SetCode)
	if err != nil {
		return nil, err
	}
	releaseDate := set.ReleaseDate
	if co.OriginalReleaseDate != "" {
		releaseDate = co.OriginalReleaseDate
	}
	setDate, err := time.Parse("2006-01-02", releaseDate)
	if err != nil {
		return nil, err
	}
	return &SortingData{
		co:          co,
		releaseDate: setDate,
		parentCode:  set.ParentCode,
	}, nil
}

func sortSets(uuidI, uuidJ string) bool {
	sortingI, err := getSortingData(uuidI)
	if err != nil {
		return false
	}
	sortingJ, err := getSortingData(uuidJ)
	if err != nil {
		return false
	}
	cI, setDateI := sortingI.co, sortingI.releaseDate
	cJ, setDateJ := sortingJ.co, sortingJ.releaseDate

	// If the two sets have the same release date, let's dig more
	if setDateI.Equal(setDateJ) {
		// If they are part of the same edition, check for their collector number
		// taking their foiling into consideration
		if cI.Edition == cJ.Edition {
			// Special case for sealed products
			if cI.Sealed && cJ.Sealed {
				return cI.Name < cJ.Name
			}

			// If their number is the same, check for foiling status
			if cI.Card.Number == cJ.Card.Number {
				if cI.Etched || cJ.Etched {
					if cI.Etched == true && cJ.Etched == false {
						return false
					} else if cI.Etched == false && cJ.Etched == true {
						return true
					}
				} else if cI.Foil || cJ.Foil {
					if cI.Foil == true && cJ.Foil == false {
						return false
					} else if cI.Foil == false && cJ.Foil == true {
						return true
					}
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
		} else if sortingI.parentCode == "" && sortingJ.parentCode != "" {
			return true
		} else if sortingJ.parentCode == "" && sortingI.parentCode != "" {
			return false
		} else {
			return cI.Edition < cJ.Edition
		}
	}

	return setDateI.After(setDateJ)
}

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type SearchConfig struct {
	// The search strategy to be used
	SearchMode string

	// Only for SearchMode == "hashing"
	UUIDs []string

	// Name of the card being searched (may be blank)
	CleanQuery string

	// Static options to be parsed during various steps
	Options map[string]string

	// Chain of filters to be applied to card filtering
	CardFilters []FilterElem

	// Chain of filters to be applied to scraper filtering
	StoreFilters []FilterStoreElem

	// Chain of filters to be applied to single prices
	PriceFilters []FilterPriceElem
}

type FilterElem struct {
	Name   string
	Negate bool
	Values []string
}

type FilterStoreElem struct {
	Name   string
	Negate bool
	Values []string

	OnlyForSeller bool
	OnlyForVendor bool
}

type FilterPriceElem struct {
	Name   string
	Negate bool
	Value  float64

	// Function used to derive a store price
	Price4Store func(string, string) float64

	// All stores sources present in the map
	Stores []string

	// Cache of cardId:prices used in the filter
	PriceCache map[string][]float64

	OnlyForSeller bool
	OnlyForVendor bool
}

// Return a comma-separated string of set codes, from a comma-separated
// list of codes or edition names. If no match is found, the input code
// segment is returned as-is.
func fixupEditionNG(code string) []string {
	var out []string

	code = strings.TrimSpace(code)
	for _, field := range strings.Split(code, ",") {
		field = strings.TrimPrefix(field, "\"")
		field = strings.TrimSuffix(field, "\"")

		set, err := mtgmatcher.GetSet(field)
		if err == nil {
			out = append(out, set.Code)
			continue
		}
		set, err = mtgmatcher.GetSetByName(field)
		if err == nil {
			out = append(out, set.Code)
			continue
		}
		// Not found, return as-is
		out = append(out, field)
	}
	return out
}

func fixupStoreCodeNG(code string) []string {
	code = strings.ToUpper(code)
	filters := strings.Split(code, ",")
	for i := range filters {
		switch filters[i] {
		case "CT":
			filters[i] = CT_STANDARD
		case "CT0":
			filters[i] = CT_ZERO
		case "MKM_LOW":
			filters[i] = MKM_LOW
		case "MKM_TREND":
			filters[i] = MKM_TREND
		case "TCG_LOW":
			filters[i] = TCG_LOW
		case "TCG_MARKET":
			filters[i] = TCG_MARKET
		case "TCG_PLAYER":
			filters[i] = TCG_MAIN
		case "TCG_DIRECT":
			filters[i] = TCG_DIRECT
		}
		filters[i] = strings.ToLower(filters[i])
	}
	return filters
}

func fixupRarityNG(code string) []string {
	code = strings.ToLower(code)
	filters := strings.Split(code, ",")
	for i := range filters {
		switch filters[i] {
		case "c":
			filters[i] = "common"
		case "u":
			filters[i] = "uncommon"
		case "r":
			filters[i] = "rare"
		case "m":
			filters[i] = "mythic"
		case "s":
			filters[i] = "special"
		}
	}
	return filters
}

func fixupFinishNG(code string) []string {
	return strings.Split(strings.ToLower(code), ",")
}

func fixupTypeNG(code string) []string {
	filters := strings.Split(code, ",")
	for i := range filters {
		filters[i] = strings.Title(filters[i])
	}
	return filters
}

var re *regexp.Regexp

var FilterOperations = map[string][]string{
	"sm":        []string{":"},
	"skip":      []string{":"},
	"s":         []string{":"},
	"cn":        []string{":", ">", "<"},
	"date":      []string{":", ">", "<"},
	"r":         []string{":"},
	"t":         []string{":"},
	"f":         []string{":"},
	"c":         []string{":"},
	"price":     []string{">", "<"},
	"buy_price": []string{">", "<"},
	"arb_price": []string{">", "<"},
	"rev_price": []string{">", "<"},
	"store":     []string{":"},
	"seller":    []string{":"},
	"vendor":    []string{":"},
	"region":    []string{":"},
}

func init() {
	var regexpOptions string
	var opts []string

	for key := range FilterOperations {
		opts = append(opts, key)
	}
	// Sort keys by shorter and alphabetical (since they may be the more common)
	sort.Slice(opts, func(i, j int) bool {
		if len(opts[i]) == len(opts[j]) {
			return opts[i] < opts[j]
		}
		return len(opts[i]) < len(opts[j])
	})

	regexpOptions = fmt.Sprintf(`-?(%s)[:<>](("([^"]+)"|\S+))+`, strings.Join(opts, "|"))

	re = regexp.MustCompile(regexpOptions)
}

func parseSearchOptionsNG(query string, blocklistRetail, blocklistBuylist []string) (config SearchConfig) {
	var filters []FilterElem
	var filterStores []FilterStoreElem
	var filterPrices []FilterPriceElem
	options := map[string]string{}

	// Apply blocklists as if they were options, need to pass them through
	// the fixup due to upper/lower casing
	// This needs to be the first element for performance and for supporting
	// hashing searches
	if blocklistRetail != nil {
		filterStores = append(filterStores, FilterStoreElem{
			Name:          "seller",
			Negate:        true,
			Values:        fixupStoreCodeNG(strings.Join(blocklistRetail, ",")),
			OnlyForSeller: true,
		})
	}
	if blocklistBuylist != nil {
		filterStores = append(filterStores, FilterStoreElem{
			Name:          "vendor",
			Negate:        true,
			Values:        fixupStoreCodeNG(strings.Join(blocklistBuylist, ",")),
			OnlyForVendor: true,
		})
	}

	// Support our UUID style when there are no options to parse
	if !strings.Contains(query, ":") {
		fields := strings.Split(query, ",")
		for _, field := range fields {
			field = strings.TrimSpace(field)
			co, err := mtgmatcher.GetUUID(field)
			if err != nil {
				// XXX: Scryfall id reports the first finish available
				field = mtgmatcher.Scryfall2UUID(field)
				co, err = mtgmatcher.GetUUID(field)
				if err != nil {
					continue
				}
			}
			// Save the last name found
			query = co.Name
			config.SearchMode = "hashing"
			config.UUIDs = append(config.UUIDs, field)
		}

		// Early return if hash was found
		if config.SearchMode != "" {
			// When multiple fields are requested it's impossible to rebuild
			// the query, so just ignore it
			if len(fields) != 1 {
				query = ""
			}
			config.CleanQuery = query
			config.StoreFilters = filterStores
			return
		}
	}

	// Support Scryfall bot syntax
	ogQuery := query
	if strings.Contains(query, "|") {
		elements := strings.Split(query, "|")
		query = elements[0]
		if len(elements) > 1 {
			query += " s:" + elements[1]
		}
		if len(elements) > 2 {
			query += " cn:" + elements[2]
		}
	}

	// Filter out the finish shortcut suffix
	if strings.HasSuffix(ogQuery, "&") {
		query = strings.TrimSuffix(query, "&")
		query += " f:nonfoil"
	} else if strings.HasSuffix(ogQuery, "*") {
		query = strings.TrimSuffix(query, "*")
		query += " f:foil"
	} else if strings.HasSuffix(ogQuery, "~") {
		query = strings.TrimSuffix(query, "~")
		query += " f:etched"
	}

	// Iterate over the various possible filters
	fields := re.FindAllString(query, -1)
	for _, field := range fields {
		query = strings.Replace(query, field, "", 1)

		index := strings.Index(field, ":")
		if index == -1 {
			index = strings.Index(field, "<")
		}
		if index == -1 {
			index = strings.Index(field, ">")
		}
		// Safety check
		if index == -1 {
			continue
		}

		option := field[:index]
		operation := string(field[index])
		code := field[index+1:]

		negate := false
		if strings.HasPrefix(option, "-") {
			option = strings.TrimPrefix(option, "-")
			negate = true
		}

		// Check the operation is allowed on the given option
		if !SliceStringHas(FilterOperations[option], operation) {
			continue
		}

		switch option {
		// Options that modify the search engine
		case "sm":
			config.SearchMode = strings.ToLower(code)
		case "skip":
			options["skip"] = strings.ToLower(code)

		// Options that modify the card searches
		case "s":
			filters = append(filters, FilterElem{
				Name:   "edition",
				Negate: negate,
				Values: fixupEditionNG(code),
			})
		case "cn":
			opt := "number"
			if operation == ">" {
				opt = "number_greater_than"
			} else if operation == "<" {
				opt = "number_less_than"
			}
			filters = append(filters, FilterElem{
				Name:   opt,
				Negate: negate,
				Values: strings.Split(code, ","),
			})
		case "r":
			filters = append(filters, FilterElem{
				Name:   "rarity",
				Negate: negate,
				Values: fixupRarityNG(code),
			})
		case "f":
			filters = append(filters, FilterElem{
				Name:   "finish",
				Negate: negate,
				Values: fixupFinishNG(code),
			})
		case "t":
			filters = append(filters, FilterElem{
				Name:   "type",
				Negate: negate,
				Values: fixupTypeNG(code),
			})
		case "date":
			opt := "date"
			switch operation {
			case ">":
				opt = "date_greater_than"
			case "<":
				opt = "date_less_than"
			}
			filters = append(filters, FilterElem{
				Name:   opt,
				Negate: negate,
				Values: []string{fixupDate(code)},
			})

		// Options that modify the searched scrapers
		case "store", "seller", "vendor":
			filterStores = append(filterStores, FilterStoreElem{
				Name:          option,
				Negate:        negate,
				Values:        fixupStoreCodeNG(code),
				OnlyForSeller: option == "seller",
				OnlyForVendor: option == "vendor",
			})
		case "region":
			filterStores = append(filterStores, FilterStoreElem{
				Name:   option,
				Negate: negate,
				Values: strings.Split(strings.ToLower(code), ","),
			})

		// Pricing Options
		case "c":
			options["condition"] = strings.ToUpper(code)
		case "price", "buy_price", "arb_price", "rev_price":
			var isSeller, isVendor bool
			var price4store func(string, string) float64
			// Each of these entries applies to either retail or buylist
			// and needs different price sources for comparisons
			switch option {
			case "price":
				isSeller = true
				price4store = price4seller
			case "buy_price":
				isVendor = true
				price4store = price4vendor
			case "arb_price":
				isSeller = true
				price4store = price4vendor
			case "rev_price":
				isVendor = true
				price4store = price4seller
			}
			var optName string
			switch operation {
			case ">":
				optName = option + "_greater_than"
			case "<":
				optName = option + "_less_than"
			}
			filter := FilterPriceElem{
				Name:          optName,
				Negate:        negate,
				OnlyForSeller: isSeller,
				OnlyForVendor: isVendor,
				Price4Store:   price4store,
			}

			// If code is a price, just keep it, otherwise parse stores later
			// (because this needs to know which card to compare against)
			price, err := strconv.ParseFloat(code, 64)
			if err == nil {
				filter.Value = price
			} else {
				filter.Stores = fixupStoreCodeNG(code)
			}
			filter.PriceCache = map[string][]float64{}
			filterPrices = append(filterPrices, filter)
		}
	}

	config.CleanQuery = strings.TrimSpace(query)
	config.Options = options
	config.CardFilters = filters
	config.StoreFilters = filterStores
	config.PriceFilters = filterPrices

	return
}

func compareCollectorNumber(filters []string, co *mtgmatcher.CardObject, cmpFunc func(a, b int) bool) bool {
	if filters == nil {
		return false
	}
	value := filters[0]

	ref, errR := strconv.Atoi(co.Number)
	num, errN := strconv.Atoi(value)
	if errR != nil || errN != nil {
		return false
	}

	if cmpFunc(num, ref) {
		return true
	}

	return false
}

func parseCardDate(co *mtgmatcher.CardObject) (time.Time, error) {
	cardDateStr := co.OriginalReleaseDate
	if cardDateStr == "" {
		set, err := mtgmatcher.GetSet(co.SetCode)
		if err == nil {
			cardDateStr = set.ReleaseDate
		}
	}
	return time.Parse("2006-01-02", cardDateStr)
}

func compareReleaseDate(filters []string, co *mtgmatcher.CardObject, cmpFunc func(a, b time.Time) bool) bool {
	if filters == nil {
		return false
	}
	value := filters[0]

	releaseDate, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}

	cardDate, err := parseCardDate(co)
	if err != nil {
		return false
	}
	if cmpFunc(cardDate, releaseDate) {
		return true
	}
	return true
}

var FilterCardFuncs = map[string]func(filters []string, co *mtgmatcher.CardObject) bool{
	"edition": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !SliceStringHas(filters, co.SetCode)
	},
	"rarity": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !SliceStringHas(filters, co.Rarity)
	},
	"type": func(filters []string, co *mtgmatcher.CardObject) bool {
		for _, value := range filters {
			if SliceStringHas(co.Subtypes, value) ||
				SliceStringHas(co.Types, value) ||
				SliceStringHas(co.Supertypes, value) {
				return false
			}
		}
		return true
	},
	"number": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !SliceStringHas(filters, co.Number)
	},
	"number_greater_than": func(filters []string, co *mtgmatcher.CardObject) bool {
		return compareCollectorNumber(filters, co, func(a, b int) bool {
			return a > b
		})
	},
	"number_less_than": func(filters []string, co *mtgmatcher.CardObject) bool {
		return compareCollectorNumber(filters, co, func(a, b int) bool {
			return a < b
		})
	},
	"finish": func(filters []string, co *mtgmatcher.CardObject) bool {
		for _, value := range filters {
			switch value {
			case "etched":
				if co.Etched {
					return false
				}
			case "foil":
				if co.Foil {
					return false
				}
			case "nonfoil":
				if !co.Foil && !co.Etched {
					return false
				}
			}
		}
		return true
	},
	"date": func(filters []string, co *mtgmatcher.CardObject) bool {
		return compareReleaseDate(filters, co, func(a, b time.Time) bool {
			return !a.Equal(b)
		})
	},
	"date_greater_than": func(filters []string, co *mtgmatcher.CardObject) bool {
		return compareReleaseDate(filters, co, func(a, b time.Time) bool {
			return a.Before(b)
		})
	},
	"date_less_than": func(filters []string, co *mtgmatcher.CardObject) bool {
		return compareReleaseDate(filters, co, func(a, b time.Time) bool {
			return a.After(b)
		})
	},
}

func shouldSkipCardNG(cardId string, filters []FilterElem) bool {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return true
	}

	for i := range filters {
		res := FilterCardFuncs[filters[i].Name](filters[i].Values, co)
		if filters[i].Negate {
			res = !res
		}
		if res {
			return true
		}
	}

	return false
}

func localizeScraper(filters []string, scraper mtgban.Scraper) bool {
	for _, value := range filters {
		switch value {
		case "us":
			if scraper.Info().CountryFlag == "" {
				return false
			}
		case "eu":
			if scraper.Info().CountryFlag == "EU" {
				return false
			}
		case "jp":
			if scraper.Info().CountryFlag == "JP" {
				return false
			}
		}
	}
	return true
}

var FilterStoreFuncs = map[string]func(filters []string, scraper mtgban.Scraper) bool{
	"store": func(filters []string, scraper mtgban.Scraper) bool {
		return !SliceStringHas(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"seller": func(filters []string, scraper mtgban.Scraper) bool {
		_, ok := scraper.(mtgban.Seller)
		return ok && !SliceStringHas(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"vendor": func(filters []string, scraper mtgban.Scraper) bool {
		_, ok := scraper.(mtgban.Vendor)
		return ok && !SliceStringHas(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"region": func(filters []string, scraper mtgban.Scraper) bool {
		return localizeScraper(filters, scraper)
	},
	"region_keep_index": func(filters []string, scraper mtgban.Scraper) bool {
		if scraper.Info().MetadataOnly {
			return false
		}
		return localizeScraper(filters, scraper)
	},
}

func shouldSkipStoreNG(scraper mtgban.Scraper, filters []FilterStoreElem) bool {
	if scraper == nil {
		return true
	}

	for i := range filters {
		// Do not call functions that do not apply to certain elements,
		// or the negate step might thwart results
		_, isSeller := scraper.(mtgban.Seller)
		_, isVendor := scraper.(mtgban.Vendor)
		if filters[i].OnlyForSeller && !isSeller {
			continue
		} else if filters[i].OnlyForVendor && !isVendor {
			continue
		}

		res := FilterStoreFuncs[filters[i].Name](filters[i].Values, scraper)
		if filters[i].Negate {
			res = !res
		}
		if res {
			return true
		}
	}

	return false
}

// These functions include the referenced Price so that users can visualize it
func priceGreaterThan(filters []float64, refPrice float64) bool {
	for i := range filters {
		if filters[i] <= refPrice {
			return false
		}
	}
	return true
}

func priceLessThan(filters []float64, refPrice float64) bool {
	for i := range filters {
		if filters[i] >= refPrice {
			return false
		}
	}
	return true
}

var FilterPriceFuncs = map[string]func(filters []float64, refPrice float64) bool{
	"price_greater_than":     priceGreaterThan,
	"price_less_than":        priceLessThan,
	"buy_price_greater_than": priceGreaterThan,
	"buy_price_less_than":    priceLessThan,
	"arb_price_greater_than": priceGreaterThan,
	"arb_price_less_than":    priceLessThan,
	"rev_price_greater_than": priceGreaterThan,
	"rev_price_less_than":    priceLessThan,
}

func shouldSkipPriceNG(cardId string, entry mtgban.GenericEntry, filters []FilterPriceElem) bool {
	if entry.Pricing() == 0 {
		return true
	}

	for i := range filters {
		// Do not call functions that do not apply to certain elements
		_, isSeller := entry.(mtgban.InventoryEntry)
		_, isVendor := entry.(mtgban.BuylistEntry)
		if filters[i].OnlyForSeller && !isSeller {
			continue
		} else if filters[i].OnlyForVendor && !isVendor {
			continue
		}

		// Check if we already have prices for this card
		_, found := filters[i].PriceCache[cardId]
		if !found {
			// If there is no set value, then look it up with the price4store function
			if filters[i].Value == 0 {
				filters[i].PriceCache[cardId] = make([]float64, 0, len(filters[i].Stores))
				for j := range filters[i].Stores {
					price := filters[i].Price4Store(cardId, filters[i].Stores[j])
					filters[i].PriceCache[cardId] = append(filters[i].PriceCache[cardId], price)
				}
			} else {
				// Else fill in the cache with the same price
				filters[i].PriceCache[cardId] = []float64{filters[i].Value}
			}
		}

		res := FilterPriceFuncs[filters[i].Name](filters[i].PriceCache[cardId], entry.Pricing())
		if filters[i].Negate {
			res = !res
		}
		if res {
			return true
		}
	}

	return false
}

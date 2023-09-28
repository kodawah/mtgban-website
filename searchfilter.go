package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
	"github.com/mtgban/go-mtgban/mtgmatcher/mtgjson"
	"golang.org/x/exp/slices"
)

type SearchConfig struct {
	// The search strategy to be used
	SearchMode string

	// Sort strategy
	SortMode string

	// Only for SearchMode == "hashing"
	UUIDs []string

	// Name of the card being searched (may be blank)
	CleanQuery string

	// Full query searched (may be blank)
	FullQuery string

	// Chain of filters to be applied to card filtering
	CardFilters []FilterElem

	// Chain of filters to be applied to scraper filtering
	StoreFilters []FilterStoreElem

	// Chain of filters to be applied to single prices
	PriceFilters []FilterPriceElem

	// Chain of filters to be applied to entries
	EntryFilters []FilterEntryElem

	// Skip retail searches entirely
	SkipRetail bool

	// Skip buylist searches entirely
	SkipBuylist bool

	// Skip card entry if no retail price was found
	SkipEmptyRetail bool

	// Skip card entry if no buylist price was found
	SkipEmptyBuylist bool
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

type FilterEntryElem struct {
	Name   string
	Negate bool
	Values []string

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
		case "TCG_DIRECT_NET":
			filters[i] = TCG_DIRECT_NET
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
		case "t":
			filters[i] = "token"
		case "o":
			filters[i] = "oversize"
		}
	}
	return filters
}

func fixupNumberNG(code string) []string {
	code = strings.ToLower(code)
	filters := strings.Split(code, ",")
	for i := range filters {
		filters[i] = strings.TrimLeft(filters[i], "0")
	}
	return filters
}

func fixupFinishNG(code string) []string {
	return strings.Split(strings.ToLower(code), ",")
}

func fixupTypeNG(code string) []string {
	filters := strings.Split(code, ",")
	for i := range filters {
		filters[i] = mtgmatcher.Title(filters[i])
	}
	return filters
}

func fixupDateNG(code string) string {
	set, err := mtgmatcher.GetSet(strings.ToUpper(code))
	if err == nil {
		code = set.ReleaseDate
	}
	_, err = time.Parse("2006-01-02", code)
	if err == nil {
		return code
	}
	return ""
}

var colorMap = map[string][]string{
	"c":           {},
	"colorless":   {},
	"white":       {"W"},
	"blue":        {"U"},
	"black":       {"B"},
	"red":         {"R"},
	"green":       {"G"},
	"azorius":     {"W", "U"},
	"dimir":       {"U", "B"},
	"rakdos":      {"B", "R"},
	"gruul":       {"R", "G"},
	"selesnya":    {"G", "W"},
	"orzhov":      {"W", "B"},
	"izzet":       {"U", "R"},
	"golgari":     {"B", "G"},
	"boros":       {"R", "W"},
	"simic":       {"G", "U"},
	"bant":        {"G", "W", "U"},
	"esper":       {"W", "U", "B"},
	"grixis":      {"U", "B", "R"},
	"jund":        {"B", "G", "R"},
	"naya":        {"R", "G", "W"},
	"abzan":       {"W", "B", "G"},
	"jeskai":      {"U", "R", "W"},
	"sultai":      {"B", "G", "U"},
	"mardu":       {"R", "W", "B"},
	"temur":       {"G", "U", "R"},
	"lorehold":    {"R", "W"},
	"prismari":    {"U", "R"},
	"quandrix":    {"B", "G"},
	"silverquill": {"U", "R"},
	"witherbloom": {"B", "G"},
	"chaos":       {"B", "G", "R", "U"},
	"aggression":  {"B", "G", "R", "W"},
	"altruism":    {"G", "R", "U", "W"},
	"growth":      {"B", "G", "U", "W"},
	"artifice":    {"B", "R", "U", "W"},
	"m":           {"W", "U", "B", "R", "G"},
	"multi":       {"W", "U", "B", "R", "G"},
	"multicolor":  {"W", "U", "B", "R", "G"},
}

func fixupColorNG(code string) []string {
	colors, found := colorMap[strings.ToLower(code)]
	if found {
		return colors
	}
	return strings.Split(strings.ToUpper(code), "")
}

// Validate UUIDs, convert them to mtgban format
func fixupIDs(code string) []string {
	fields := strings.Split(code, ",")
	for i, field := range fields {
		var uuid string
		_, err := mtgmatcher.GetUUID(field)
		if err == nil {
			continue
		}
		// XXX: id funcs report the first finish available
		uuid = mtgmatcher.Scryfall2UUID(field)
		if uuid != "" {
			fields[i] = uuid
			continue
		}
		uuid = mtgmatcher.Tcg2UUID(field)
		if uuid != "" {
			fields[i] = uuid
			continue
		}
	}
	return fields
}

func sealedname2uuid(name string) string {
	name = strings.TrimLeft(strings.TrimRight(name, "\" "), "\" ")
	res, err := mtgmatcher.SearchSealedEquals(name)
	if err != nil {
		return ""
	}
	return res[0]
}

func cardobject2sources(co *mtgmatcher.CardObject) []string {
	var values []string
	if co.Sealed {
		values = co.SourceProducts["sealed"]
	} else if co.Etched {
		values = co.SourceProducts["etched"]
	} else if co.Foil {
		values = co.SourceProducts["foil"]
	} else {
		values = co.SourceProducts["nonfoil"]
	}
	return values
}

func fixupPicks(code string) []string {
	co, err := mtgmatcher.GetUUID(code)
	if err != nil {
		co, err = mtgmatcher.GetUUID(sealedname2uuid(code))
		if err != nil {
			return []string{}
		}
	}
	if !co.Sealed {
		return []string{}
	}

	picks, err := mtgmatcher.GetPicksForSealed(co.SetCode, co.UUID)
	if err != nil {
		return []string{}
	}

	return picks
}

func fixupContents(code string) []string {
	co, err := mtgmatcher.GetUUID(code)
	if err != nil {
		co, err = mtgmatcher.GetUUID(sealedname2uuid(code))
		if err != nil {
			return []string{}
		}
	}
	if !co.Sealed {
		return []string{}
	}

	return []string{co.UUID}
}

func fixupContainer(code string) []string {
	co, err := mtgmatcher.GetUUID(code)
	if err != nil {
		results, err := mtgmatcher.SearchEquals(code)
		if err != nil {
			return []string{}
		}

		var sources []string
		for _, result := range results {
			co, err := mtgmatcher.GetUUID(result)
			if err != nil {
				continue
			}
			sources = append(sources, cardobject2sources(co)...)
		}
		return sources
	}

	return cardobject2sources(co)
}

func price4seller(cardId, shorthand string) float64 {
	for _, seller := range Sellers {
		if seller != nil && strings.EqualFold(seller.Info().Shorthand, shorthand) {
			inv, err := seller.Inventory()
			if err != nil {
				continue
			}
			entries, found := inv[cardId]
			if !found {
				continue
			}
			return entries[0].Price
		}
	}
	return 0
}

func price4vendor(cardId, shorthand string) float64 {
	for _, vendor := range Vendors {
		if vendor != nil && strings.EqualFold(vendor.Info().Shorthand, shorthand) {
			bl, err := vendor.Buylist()
			if err != nil {
				continue
			}
			entries, found := bl[cardId]
			if !found {
				continue
			}
			return entries[0].BuyPrice
		}
	}
	return 0
}

var re *regexp.Regexp

var FilterOperations = map[string][]string{
	"sm":        []string{":"},
	"skip":      []string{":"},
	"sort":      []string{":"},
	"edition":   []string{":"},
	"s":         []string{":"},
	"se":        []string{":"},
	"number":    []string{":", ">", "<"},
	"cn":        []string{":", ">", "<"},
	"cne":       []string{":"},
	"date":      []string{":", ">", "<"},
	"r":         []string{":"},
	"t":         []string{":"},
	"f":         []string{":"},
	"c":         []string{":"},
	"color":     []string{":"},
	"unpack":    []string{":"},
	"contents":  []string{":"},
	"container": []string{":"},
	"ci":        []string{":"},
	"identity":  []string{":"},
	"cond":      []string{":"},
	"condr":     []string{":"},
	"condb":     []string{":"},
	"id":        []string{":"},
	"is":        []string{":"},
	"not":       []string{":"},
	"on":        []string{":"},
	"price":     []string{">", "<"},
	"buy_price": []string{">", "<"},
	"arb_price": []string{">", "<"},
	"rev_price": []string{">", "<"},
	"store":     []string{":"},
	"seller":    []string{":"},
	"aseller":   []string{":"},
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
	var filterEntries []FilterEntryElem

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
	if !strings.Contains(query, ":") && !strings.Contains(query, "|") {
		// XXX should use the idlookup filter
		uuids := fixupIDs(query)
		for _, uuid := range uuids {
			co, err := mtgmatcher.GetUUID(uuid)
			if err != nil {
				continue
			}

			// Save the last name found
			config.CleanQuery = co.Name
			// Rebuild the full query for this card
			config.FullQuery = co.Name
			if !co.Sealed {
				config.FullQuery += " s:" + co.SetCode + " cn:" + co.Number
				if co.Etched {
					config.FullQuery += " f:etched"
				} else if co.Foil {
					config.FullQuery += " f:foil"
				}
			}

			// Set the special search mode and its data source
			config.SearchMode = "hashing"
			config.UUIDs = append(config.UUIDs, uuid)
		}

		// Early return if hash was found
		if config.SearchMode != "" {
			// When multiple fields are requested it's impossible to rebuild
			// the query, so just ignore it
			if len(config.UUIDs) != 1 {
				config.CleanQuery = ""
				config.FullQuery = ""
			}
			config.StoreFilters = filterStores
			return
		}
	}

	// Clean any special characters from the main query, handle it later
	var lastChar string
	if strings.HasSuffix(query, "&") || strings.HasSuffix(query, "*") || strings.HasSuffix(query, "&") {
		lastChar = query[len(query)-1:]
		query = strings.TrimRight(query, "&*~")
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
		if !slices.Contains(FilterOperations[option], operation) {
			continue
		}

		switch option {
		// Options that modify the search engine
		case "sm":
			config.SearchMode = strings.ToLower(code)
		case "skip":
			switch strings.ToLower(code) {
			case "retail":
				config.SkipRetail = true
			case "buylist":
				config.SkipBuylist = true
			case "nosales":
				config.SkipEmptyRetail = true
			case "nobuys":
				config.SkipEmptyBuylist = true
			case "empty":
				config.SkipEmptyRetail = true
				config.SkipEmptyBuylist = true
			}
		case "sort":
			code = strings.ToLower(code)
			switch code {
			case "chrono", "alpha", "retail", "buylist":
				config.SortMode = code
			}

		// Options that modify the card searches
		case "s", "edition":
			filters = append(filters, FilterElem{
				Name:   "edition",
				Negate: negate,
				Values: fixupEditionNG(code),
			})
		case "se":
			filters = append(filters, FilterElem{
				Name:   "edition_regexp",
				Negate: negate,
				Values: []string{code},
			})
		case "cn", "number":
			opt := "number"
			if operation == ">" {
				opt = "number_greater_than"
			} else if operation == "<" {
				opt = "number_less_than"
			}
			filters = append(filters, FilterElem{
				Name:   opt,
				Negate: negate,
				Values: fixupNumberNG(code),
			})
		case "cne":
			filters = append(filters, FilterElem{
				Name:   "number_regexp",
				Negate: negate,
				// No fixup because we need to trust input
				Values: []string{code},
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
		case "is", "not":
			if option == "not" {
				negate = !negate
			}
			filters = append(filters, FilterElem{
				Name:   "is",
				Negate: negate,
				Values: strings.Split(strings.ToLower(code), ","),
			})
		case "on":
			filters = append(filters, FilterElem{
				Name:   "on",
				Negate: negate,
				Values: strings.Split(strings.ToLower(code), ","),
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
				Values: []string{fixupDateNG(code)},
			})
		case "c", "color", "ci", "identity":
			opt := "color"
			if option == "ci" || option == "color_identity" {
				opt = "color_identity"
			}
			filters = append(filters, FilterElem{
				Name:   opt,
				Negate: negate,
				Values: fixupColorNG(code),
			})
		case "id":
			filters = append(filters, FilterElem{
				Name:   "idlookup",
				Negate: negate,
				Values: fixupIDs(code),
			})
		case "unpack":
			filters = append(filters, FilterElem{
				Name:   "idlookup",
				Negate: negate,
				Values: fixupPicks(code),
			})
		case "contents":
			config.SearchMode = "mixed"
			filters = append(filters, FilterElem{
				Name:   "contents",
				Negate: negate,
				Values: fixupContents(code),
			})
		case "container":
			filters = append(filters, FilterElem{
				Name:   "idlookup",
				Negate: negate,
				Values: fixupContainer(code),
			})

		// Options that modify the searched scrapers
		case "store", "seller", "aseller", "vendor":
			var isSeller, isVendor bool
			// Skip empty result entries when filtering by either option
			switch option {
			case "aseller":
				config.SkipEmptyRetail = true
				isSeller = true
				option = "seller"
			case "seller":
				config.SkipEmptyRetail = true
				isSeller = true
				option = "seller_keep_index"
				// When filtering out, use the more generic function
				if negate {
					option = "seller"
				}
			case "buylist":
				config.SkipEmptyBuylist = true
				isVendor = true
			}
			filterStores = append(filterStores, FilterStoreElem{
				Name:          option,
				Negate:        negate,
				Values:        fixupStoreCodeNG(code),
				OnlyForSeller: isSeller,
				OnlyForVendor: isVendor,
			})
		case "region":
			filterStores = append(filterStores, FilterStoreElem{
				Name:   option,
				Negate: negate,
				Values: strings.Split(strings.ToLower(code), ","),
			})

		// Pricing Options
		case "cond", "condr", "condb":
			filterEntries = append(filterEntries, FilterEntryElem{
				Name:          "condition",
				Negate:        negate,
				Values:        strings.Split(strings.ToUpper(code), ","),
				OnlyForSeller: option == "condr",
				OnlyForVendor: option == "condb",
			})
		case "price", "buy_price", "arb_price", "rev_price":
			var isSeller, isVendor bool
			var price4store func(string, string) float64
			// Each of these entries applies to either retail or buylist
			// and needs different price sources for comparisons
			switch option {
			case "price":
				isSeller = true
				price4store = price4seller
				config.SkipEmptyRetail = true
			case "buy_price":
				isVendor = true
				price4store = price4vendor
				config.SkipEmptyBuylist = true
			case "arb_price":
				isSeller = true
				price4store = price4vendor
				config.SkipEmptyRetail = true
			case "rev_price":
				isVendor = true
				price4store = price4seller
				config.SkipEmptyBuylist = true
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

	// Check if we can apply a finish filter through the custom syntax
	// or restore the original regexp if it's the last element
	if lastChar != "" {
		lastElementIsRegexp := len(filters) > 0 && strings.HasSuffix(filters[len(filters)-1].Name, "regexp")
		if lastElementIsRegexp && len(filters[len(filters)-1].Values) > 0 {
			filters[len(filters)-1].Values[0] += lastChar
		} else {
			var finish string
			switch lastChar {
			case "&":
				finish = "nonfoil"
			case "*":
				finish = "foil"
			case "~":
				finish = "etched"
			}
			filters = append(filters, FilterElem{
				Name:   "finish",
				Values: []string{finish},
			})
		}
	}

	// Support Scryfall bot syntax only when the search mode is not set
	if config.SearchMode == "" && strings.Contains(query, "|") {
		elements := strings.Split(query, "|")
		query = elements[0]
		extraQuery := strings.TrimSpace(elements[0])
		if len(elements) > 1 {
			extraQuery += " s:" + strings.TrimSpace(elements[1])
		}
		if len(elements) > 2 {
			extraQuery += " cn:" + strings.TrimSpace(elements[2])
		}
		extraConfig := parseSearchOptionsNG(extraQuery, nil, nil)
		filters = append(filters, extraConfig.CardFilters...)
	}

	config.CleanQuery = strings.TrimSpace(query)
	config.CardFilters = filters
	config.StoreFilters = filterStores
	config.PriceFilters = filterPrices
	config.EntryFilters = filterEntries

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
		return true
	}

	return cmpFunc(num, ref)
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

func findCommanderInDeck(sealedUUID string) []string {
	var output []string

	sealed, err := mtgmatcher.GetUUID(sealedUUID)
	if err != nil {
		return nil
	}

	set, err := mtgmatcher.GetSet(sealed.SetCode)
	if err != nil {
		return nil
	}

	for _, product := range set.SealedProduct {
		if product.UUID != sealed.UUID {
			continue
		}
		contents, found := product.Contents["deck"]
		if !found {
			continue
		}
		for _, content := range contents {
			subset, err := mtgmatcher.GetSet(content.Set)
			if err != nil {
				continue
			}
			for _, deck := range subset.Decks {
				if deck.Name != content.Name {
					continue
				}
				for _, card := range deck.Commander {
					uuid, err := mtgmatcher.MatchId(card.UUID, card.IsFoil)
					if err != nil {
						continue
					}
					output = append(output, uuid)
				}
			}
		}
	}

	return output
}

func compareReleaseDate(filters []string, co *mtgmatcher.CardObject, cmpFunc func(a, b time.Time) bool) bool {
	if filters == nil {
		return false
	}
	value := filters[0]

	releaseDate, err := time.Parse("2006-01-02", value)
	if err != nil {
		return true
	}

	cardDate, err := parseCardDate(co)
	if err != nil {
		return true
	}

	return cmpFunc(cardDate, releaseDate)
}

var FilterCardFuncs = map[string]func(filters []string, co *mtgmatcher.CardObject) bool{
	"edition": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !slices.Contains(filters, co.SetCode)
	},
	"edition_regexp": func(filters []string, co *mtgmatcher.CardObject) bool {
		matched, _ := regexp.MatchString(filters[0], co.Edition)
		return !matched
	},
	"rarity": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !slices.Contains(filters, co.Rarity)
	},
	"type": func(filters []string, co *mtgmatcher.CardObject) bool {
		for _, value := range filters {
			if slices.Contains(co.Subtypes, value) ||
				slices.Contains(co.Types, value) ||
				slices.Contains(co.Supertypes, value) {
				return false
			}
		}
		return true
	},
	"color": func(filters []string, co *mtgmatcher.CardObject) bool {
		if len(filters) == 0 {
			return len(co.Colors) != 0
		}
		if len(filters) == 5 {
			return len(co.Colors) <= 1
		}
		for _, value := range filters {
			if !slices.Contains(co.Colors, value) {
				return true
			}
		}
		return false
	},
	"color_identity": func(filters []string, co *mtgmatcher.CardObject) bool {
		if len(filters) == 0 {
			return len(co.ColorIdentity) != 0
		}
		if len(filters) == 5 {
			return len(co.ColorIdentity) <= 1
		}
		for _, value := range co.ColorIdentity {
			if !slices.Contains(filters, value) {
				return true
			}
		}
		return false
	},
	"idlookup": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !slices.Contains(filters, co.UUID)
	},
	"contents": func(filters []string, co *mtgmatcher.CardObject) bool {
		values := cardobject2sources(co)
		for _, filter := range filters {
			if !slices.Contains(values, filter) {
				return true
			}
		}
		return false
	},
	"number": func(filters []string, co *mtgmatcher.CardObject) bool {
		return !slices.Contains(filters, strings.ToLower(co.Number))
	},
	"number_regexp": func(filters []string, co *mtgmatcher.CardObject) bool {
		matched, _ := regexp.MatchString(filters[0], co.Number)
		return !matched
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
			case "etched", "e":
				if co.Etched {
					return false
				}
			case "foil", "f":
				if co.Foil {
					return false
				}
			case "nonfoil", "nf", "r":
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
	"on": func(filters []string, co *mtgmatcher.CardObject) bool {
		for _, value := range filters {
			switch value {
			case "mtgstocks":
				_, found := Infos["STKS"][co.UUID]
				if found {
					return false
				}
			case "tcgsyp", "syp":
				_, found := Infos["TCGSYPList"][co.UUID]
				if found {
					return false
				}
			}
		}
		return true
	},
	"is": func(filters []string, co *mtgmatcher.CardObject) bool {
		for _, value := range filters {
			switch value {
			case "reserved":
				if co.IsReserved {
					return false
				}
			case "token":
				if co.Layout == "token" {
					return false
				}
			case "oversize", "oversized":
				if co.IsOversized {
					return false
				}
			case "funny":
				if co.IsFunny {
					return false
				}
			case "wcd", "gold":
				if co.BorderColor == mtgjson.BorderColorGold {
					return false
				}
			case "fullart", "fa":
				if co.IsFullArt {
					return false
				}
			case "promo":
				if co.IsPromo {
					return false
				}
			case "extendedart", "ea":
				if co.HasFrameEffect(mtgjson.FrameEffectExtendedArt) {
					return false
				}
			case "showcase", "sc", "sh":
				if co.HasFrameEffect(mtgjson.FrameEffectShowcase) {
					return false
				}
			case "borderless", "bd", "bl":
				if co.BorderColor == mtgjson.BorderColorBorderless {
					return false
				}
			case "retro", "old":
				if co.FrameVersion == "1993" || co.FrameVersion == "1997" {
					return false
				}
			case "reskin":
				if co.FlavorName != "" {
					return false
				}
			case "japanese", "jpn", "jp", "ja":
				if co.Language == mtgjson.LanguageJapanese {
					return false
				}
			case "phyrexian", "ph":
				if co.Language == mtgjson.LanguagePhyrexian {
					return false
				}
			case "commander":
				values := cardobject2sources(co)
				for _, sealedUUID := range values {
					res := findCommanderInDeck(sealedUUID)
					if slices.Contains(res, co.UUID) {
						return false
					}
				}
			default:
				// Adjust input for these known cases
				switch value {
				case "bf", "v":
					value = "boosterfun"
				case "rewards", "mpr":
					value = "playerrewards"
				case "bab", "buy-a-box":
					value = "buyabox"
				case "arena":
					value = "arenaleague"
				case "judge":
					value = "judgegift"
				case "confetti", "galaxy", "halo":
					value += "foil"
				}

				// Fall back to any promo type currently supported
				if slices.Contains(mtgjson.AllPromoTypes, value) {
					if co.HasPromoType(value) {
						return false
					}
				}

				// Finally check any leftover tags
				customTag, found := co.Identifiers["customTag"]
				if found && customTag == value {
					return false
				}
			}
		}
		return true
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
		return !slices.Contains(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"seller_keep_index": func(filters []string, scraper mtgban.Scraper) bool {
		if scraper.Info().MetadataOnly {
			return false
		}
		_, ok := scraper.(mtgban.Seller)
		return ok && !slices.Contains(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"seller": func(filters []string, scraper mtgban.Scraper) bool {
		_, ok := scraper.(mtgban.Seller)
		return ok && !slices.Contains(filters, strings.ToLower(scraper.Info().Shorthand))
	},
	"vendor": func(filters []string, scraper mtgban.Scraper) bool {
		_, ok := scraper.(mtgban.Vendor)
		return ok && !slices.Contains(filters, strings.ToLower(scraper.Info().Shorthand))
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

	_, isSeller := scraper.(mtgban.Seller)
	_, isVendor := scraper.(mtgban.Vendor)

	for i := range filters {
		// Do not call functions that do not apply to certain elements,
		// or the negate step might thwart results
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

	_, isSeller := entry.(mtgban.InventoryEntry)
	_, isVendor := entry.(mtgban.BuylistEntry)

	for i := range filters {
		// Do not call functions that do not apply to certain elements
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

var FilterEntryFuncs = map[string]func(filters []string, entry mtgban.GenericEntry) bool{
	"condition": func(filters []string, entry mtgban.GenericEntry) bool {
		return !slices.Contains(filters, entry.Condition())
	},
}

func shouldSkipEntryNG(entry mtgban.GenericEntry, filters []FilterEntryElem) bool {
	_, isSeller := entry.(mtgban.InventoryEntry)
	_, isVendor := entry.(mtgban.BuylistEntry)

	for i := range filters {
		if filters[i].OnlyForSeller && !isSeller {
			continue
		} else if filters[i].OnlyForVendor && !isVendor {
			continue
		}

		res := FilterEntryFuncs[filters[i].Name](filters[i].Values, entry)
		if filters[i].Negate {
			res = !res
		}
		if res {
			return true
		}
	}

	return false
}

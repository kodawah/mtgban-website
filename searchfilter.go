package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type FilterElem struct {
	Name   string
	Negate bool
	Values []string
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
	code = strings.ToLower(code)
	switch code {
	case "nonfoil", "foil", "etched":
	default:
		code = ""
	}
	return []string{code}
}

var FilterOperations = map[string][]string{
	"sm":        []string{":"},
	"m":         []string{":"},
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

func parseSearchOptionsNG(query string) (string, map[string]string, []FilterElem) {
	var filters []FilterElem
	options := map[string]string{}

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
			options["search_mode"] = strings.ToLower(code)
		case "m":
			options["mode"] = strings.ToLower(code)
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
				Values: []string{strings.Title(code)},
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
		case "store":
			options["scraper"] = fixupStoreCode(code)
		case "seller":
			options["seller"] = fixupStoreCode(code)
		case "vendor":
			options["vendor"] = fixupStoreCode(code)
		case "region":
			options["region"] = strings.ToLower(code)

		// Pricing Options
		case "c":
			options["condition"] = strings.ToUpper(code)
		case "price", "buy_price", "arb_price", "rev_price":
			switch operation {
			case ">":
				options[option+"_greater_than"] = fixupStoreCode(code)
			case "<":
				options[option+"_less_than"] = fixupStoreCode(code)
			}
		}
	}

	return strings.TrimSpace(query), options, filters
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
		if filters == nil {
			return false
		}
		value := filters[0]
		return !SliceStringHas(co.Subtypes, value) &&
			!SliceStringHas(co.Types, value) &&
			!SliceStringHas(co.Supertypes, value)
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
		if filters == nil {
			return false
		}
		value := filters[0]

		switch value {
		case "etched":
			if !co.Etched {
				return true
			}
		case "foil":
			if !co.Foil {
				return true
			}
		case "nonfoil":
			if co.Foil || co.Etched {
				return true
			}
		}
		return false
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

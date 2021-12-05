package main

import (
	"strconv"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type FilterElem struct {
	Name   string
	Negate bool
	Values []string
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

package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"testing"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

var NameToBeFound string
var EditionToBeFound string
var NumberToBeFound string

func TestMain(m *testing.M) {
	err := loadDatastore()
	if err != nil {
		log.Fatalln(err)
	}

	DevMode = true
	BenchMode = true

	loadScrapers()
	DatabaseLoaded = true

	sets := mtgmatcher.GetSets()
	for _, set := range sets {
		if len(set.Cards) == 0 {
			continue
		}
		index := rand.Intn(len(set.Cards))
		NameToBeFound = set.Cards[index].Name
		EditionToBeFound = set.Name
		NumberToBeFound = set.Cards[index].Number
		log.Println("Looking up", NameToBeFound, "from", set.Name, NumberToBeFound)
		break
	}

	os.Exit(m.Run())
}

func BenchmarkRegexp(b *testing.B) {
	input := fmt.Sprintf("%s sm:prefix cn:%s f:foil vendor:CK date>%s", NameToBeFound, NumberToBeFound, EditionToBeFound)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		parseSearchOptionsNG(input, nil, nil)
	}
}

func BenchmarkSearchExact(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
	}

	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchPrefix(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s sm:prefix", NameToBeFound), nil, nil)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchAllFromEdition(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("s:%s", EditionToBeFound), nil, nil)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchWithEdition(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s s:%s", NameToBeFound, EditionToBeFound), nil, nil)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchWithNumber(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s cn:%s", NameToBeFound, NumberToBeFound), nil, nil)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchWithEditionPrefix(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s s:%s sm:prefix", NameToBeFound, EditionToBeFound), nil, nil)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchOnlyRetail(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"skip": "buylist",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

func BenchmarkSearchOnlyBuylist(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"skip": "retail",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, true)
	}
}

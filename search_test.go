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

func BenchmarkSearchExact(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
	}

	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchPrefix(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"search_mode": "prefix",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchAllFromEdition(b *testing.B) {
	config := SearchConfig{
		CleanQuery: "",
		Options: map[string]string{
			"edition": EditionToBeFound,
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchWithEdition(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"edition": EditionToBeFound,
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchWithEditionPrefix(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"edition":     EditionToBeFound,
			"search_mode": "prefix",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchWithNumber(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"number": NumberToBeFound,
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil)
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
		searchParallelNG(config, nil, nil)
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
		searchParallelNG(config, nil, nil)
	}
}

func BenchmarkSearchExactNG(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
	}

	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchPrefixNG(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s sm:prefix", NameToBeFound))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchAllFromEditionNG(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("s:%s", EditionToBeFound))

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchWithEditionNG(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s s:%s", NameToBeFound, EditionToBeFound))

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchWithNumberNG(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s cn:%s", NameToBeFound, NumberToBeFound))

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchWithEditionPrefixNG(b *testing.B) {
	config := parseSearchOptionsNG(fmt.Sprintf("%s s:%s sm:prefix", NameToBeFound, EditionToBeFound))

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchOnlyRetailNG(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"skip": "buylist",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

func BenchmarkSearchOnlyBuylistNG(b *testing.B) {
	config := SearchConfig{
		CleanQuery: NameToBeFound,
		Options: map[string]string{
			"skip": "retail",
		},
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(config, nil, nil, true)
	}
}

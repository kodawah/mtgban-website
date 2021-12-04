package main

import (
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
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, nil, nil, nil)
	}
}

func BenchmarkSearchPrefix(b *testing.B) {
	options := map[string]string{
		"search_mode": "prefix",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchAllFromEdition(b *testing.B) {
	options := map[string]string{
		"edition": EditionToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel("", options, nil, nil)
	}
}

func BenchmarkSearchWithEdition(b *testing.B) {
	options := map[string]string{
		"edition": EditionToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchWithEditionPrefix(b *testing.B) {
	options := map[string]string{
		"edition":     EditionToBeFound,
		"search_mode": "prefix",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchWithNumber(b *testing.B) {
	options := map[string]string{
		"number": NumberToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchOnlyRetail(b *testing.B) {
	options := map[string]string{
		"skip": "buylist",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchOnlyBuylist(b *testing.B) {
	options := map[string]string{
		"skip": "retail",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallel(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchExactNG(b *testing.B) {
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, nil, nil, nil)
	}
}

func BenchmarkSearchPrefixNG(b *testing.B) {
	options := map[string]string{
		"search_mode": "prefix",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchAllFromEditionNG(b *testing.B) {
	options := map[string]string{
		"edition": EditionToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG("", options, nil, nil)
	}
}

func BenchmarkSearchWithEditionNG(b *testing.B) {
	options := map[string]string{
		"edition": EditionToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchWithNumberNG(b *testing.B) {
	options := map[string]string{
		"number": NumberToBeFound,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchWithEditionPrefixNG(b *testing.B) {
	options := map[string]string{
		"edition":     EditionToBeFound,
		"search_mode": "prefix",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchOnlyRetailNG(b *testing.B) {
	options := map[string]string{
		"skip": "buylist",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

func BenchmarkSearchOnlyBuylistNG(b *testing.B) {
	options := map[string]string{
		"skip": "retail",
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		searchParallelNG(NameToBeFound, options, nil, nil)
	}
}

package main

import (
	"context"
	"errors"
	"sort"

	"github.com/go-redis/redis/v8"
)

type Dataset struct {
	Name   string
	Data   []string
	Color  string
	AxisID string
	Hidden bool
	Sealed bool
}

type scraperConfig struct {
	PublicName string
	Shorthand  string
	Group      string
	Color      string
	Hidden     bool
	HasSealed  bool
}

/*
	red: 'rgb(255, 99, 132)'
	orange: 'rgb(255, 159, 64)'
	yellow: 'rgb(255, 205, 86)'
	green: 'rgb(75, 192, 192)'
	blue: 'rgb(54, 162, 235)'
	purple: 'rgb(153, 102, 255)'
	grey: 'rgb(201, 203, 207)'
	darkblue: 'rgb(23,42,72)'
*/

var enabledDatasets = []scraperConfig{
	{
		PublicName: "TCGplayer Low",
		Shorthand:  "TCGLow",
		Group:      "sellers",
		Color:      "rgb(255, 99, 132)",
		HasSealed:  true,
	},
	{
		PublicName: "TCGplayer Market",
		Shorthand:  "TCGMarket",
		Group:      "sellers",
		Color:      "rgb(255, 159, 64)",
		Hidden:     true,
	},
	{
		PublicName: "Card Kingdom Retail",
		Shorthand:  "CK",
		Group:      "sellers",
		Color:      "rgb(162, 235, 54)",
		HasSealed:  true,
	},
	{
		PublicName: "Card Kingdom Buylist",
		Shorthand:  "CK",
		Group:      "vendors",
		Color:      "rgb(54, 162, 235)",
		HasSealed:  true,
	},
	{
		PublicName: "Cardmarket Low",
		Shorthand:  "MKMLow",
		Group:      "sellers",
		Color:      "rgb(235, 205, 86)",
		HasSealed:  true,
	},
	{
		PublicName: "Cardmarket Trend",
		Shorthand:  "MKMTrend",
		Group:      "sellers",
		Color:      "rgb(201, 203, 207)",
		Hidden:     true,
	},
	{
		PublicName: "Star City Games Buylist",
		Shorthand:  "SCG",
		Group:      "vendors",
		Color:      "rgb(23,42,72)",
	},
	{
		PublicName: "ABU Games Buylist",
		Shorthand:  "ABU",
		Group:      "vendors",
		Color:      "rgb(153, 102, 255)",
	},
}

func findScraperDataIndex(group, shorthand string) int {
	for i, scraperData := range Config.Scrapers[group] {
		if scraperData.Shorthand == shorthand {
			return i
		}
	}
	return -1
}

func rdbClientForScraper(group, shorthand string) (*redis.Client, error) {
	idx := findScraperDataIndex(group, shorthand)
	if idx < 0 {
		return nil, errors.New("missing TCGLow data")
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   Config.Scrapers[group][idx].RedisIndex,
	})
	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		return nil, err
	}

	return redisClient, nil
}

// Get all the keys that will be used as x asis labels
func getDateAxisValues(cardId string) ([]string, error) {
	db, err := rdbClientForScraper("sellers", "TCGLow")
	if err != nil {
		return nil, err
	}

	keys, err := db.HKeys(context.Background(), cardId).Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		db, err = rdbClientForScraper("sellers", "TCGMarket")
		if err != nil {
			return nil, err
		}

		keys, err = db.HKeys(context.Background(), cardId).Result()
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 {
			return nil, errors.New("no data available")
		}
	}

	// Sort labels from older to newer
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	return keys, nil
}

func getDataset(cardId string, labels []string, config scraperConfig) (*Dataset, error) {
	db, err := rdbClientForScraper(config.Group, config.Shorthand)
	if err != nil {
		return nil, err
	}
	results, err := db.HGetAll(context.Background(), cardId).Result()
	if err != nil {
		return nil, err
	}

	// Fill in missing points with NaNs so that the values
	// can be mapped consistently on the chart
	data := make([]string, len(labels))
	for i := range labels {
		val, found := results[labels[i]]
		if !found {
			val = "Number.NaN"
		}
		data[i] = val
	}

	return &Dataset{
		Name:   config.PublicName,
		Data:   data,
		Color:  config.Color,
		Hidden: config.Hidden,
	}, nil
}

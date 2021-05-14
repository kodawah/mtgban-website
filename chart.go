package main

import (
	"context"
	"errors"
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
	PublicName  string
	ScraperName string
	KindName    string
	Color       string
	Hidden      bool
	HasSealed   bool
}

/*
	red: 'rgb(255, 99, 132)'
	orange: 'rgb(255, 159, 64)'
	yellow: 'rgb(255, 205, 86)'
	green: 'rgb(75, 192, 192)'
	blue: 'rgb(54, 162, 235)'
	purple: 'rgb(153, 102, 255)'
	grey: 'rgb(201, 203, 207)'
*/

var enabledDatasets = []scraperConfig{
	{
		PublicName:  "TCGplayer Low",
		ScraperName: "tcg_index",
		KindName:    TCG_LOW,
		Color:       "rgb(255, 99, 132)",
		HasSealed:   true,
	},
	{
		PublicName:  "TCGplayer Market",
		ScraperName: "tcg_index",
		KindName:    TCG_MARKET,
		Color:       "rgb(255, 159, 64)",
		Hidden:      true,
	},
	{
		PublicName:  "Card Kingdom Retail",
		ScraperName: "cardkingdom",
		KindName:    "retail",
		Color:       "rgb(162, 235, 54)",
		HasSealed:   true,
	},
	{
		PublicName:  "Card Kingdom Buylist",
		ScraperName: "cardkingdom",
		KindName:    "buylist",
		Color:       "rgb(54, 162, 235)",
		HasSealed:   true,
	},
	{
		PublicName:  "Cardmarket Low",
		ScraperName: "cardmarket",
		KindName:    MKM_LOW,
		Color:       "rgb(235, 205, 86)",
		HasSealed:   true,
	},
	{
		PublicName:  "Cardmarket Trend",
		ScraperName: "cardmarket",
		KindName:    MKM_TREND,
		Color:       "rgb(201, 203, 207)",
		Hidden:      true,
	},
}

// Get all the keys that will be used as x asis labels
func getDateAxisValues(cardId string) ([]string, error) {
	db := ScraperOptions["tcg_index"].RDBs[TCG_MARKET]
	keys, err := db.HKeys(context.Background(), cardId).Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		db = ScraperOptions["tcg_index"].RDBs[TCG_LOW]
		keys, err = db.HKeys(context.Background(), cardId).Result()
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 {
			return nil, errors.New("no data available")
		}
	}
	return keys, nil
}

func getDataset(cardId string, labels []string, config scraperConfig) (*Dataset, error) {
	db := ScraperOptions[config.ScraperName].RDBs[config.KindName]
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

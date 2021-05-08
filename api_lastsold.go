package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type PriceLastSold struct {
	Retail  float64 `json:"retail,omitempty"`
	Buylist float64 `json:"buylist,omitempty"`
}

type PriceLastSoldOutput struct {
	Error string `json:"error,omitempty"`
	Meta  struct {
		Date    time.Time `json:"date"`
		Version string    `json:"version"`
	} `json:"meta"`
	Identifiers map[string]string         `json:"identifiers,omitempty"`
	Data        map[string]*PriceLastSold `json:"data,omitempty"`

	LastSold map[string]map[string]float64 `json:"last_sold,omitempty"`
}

func PriceLastSoldAPI(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")
	out := PriceLastSoldOutput{}
	out.Meta.Date = time.Now()
	out.Meta.Version = "0.0"

	path := strings.TrimPrefix(r.URL.Path, "/api/mtg/")
	fields := strings.Split(path, "/")

	if len(fields) != 2 {
		out.Error = "Not found"
		json.NewEncoder(w).Encode(&out)
		return
	}

	allowedList := GetParamFromSig(sig, "API")
	req := fields[0]
	stores := strings.Split(req, ",")
	params := strings.Split(allowedList, ",")
	uuid := strings.TrimSuffix(fields[1], ".json")

	if req == "tcgLastSold" {
		if SigCheck && !SliceStringHas(params, "tcgLastSold") {
			out.Error = "Forbidden"
			json.NewEncoder(w).Encode(&out)
			return
		}

		out.LastSold = map[string]map[string]float64{}

		// Grab all the cached keys when none is provided
		if uuid == "" {
			var cursor uint64
			for {
				var keys []string
				var err error
				keys, cursor, err = LastSoldDB.Scan(context.Background(), cursor, "*", 10).Result()
				if err != nil {
					out.Error = err.Error()
					break
				}

				for _, cardId := range keys {
					results, err := LastSoldDB.HGetAll(context.Background(), cardId).Result()
					if err != nil {
						continue
					}
					if len(results) == 0 {
						continue
					}
					out.LastSold[cardId] = map[string]float64{}
					for cond, priceStr := range results {
						price, _ := strconv.ParseFloat(priceStr, 64)
						out.LastSold[cardId][cond] = price
					}
				}

				if cursor == 0 {
					break
				}
			}
		} else {
			results, err := LastSoldDB.HGetAll(context.Background(), uuid).Result()
			if err != nil {
				out.Error = err.Error()
			} else if len(results) != 0 {
				out.LastSold[uuid] = map[string]float64{}
				for cond, priceStr := range results {
					price, _ := strconv.ParseFloat(priceStr, 64)
					out.LastSold[uuid][cond] = price
				}
			}
		}
		json.NewEncoder(w).Encode(&out)
		return
	}

	// If there is a single unauthorized store, fail the request
	for _, store := range stores {
		if SigCheck && !SliceStringHas(params, store) && allowedList != "*" {
			out.Error = "Forbidden"
			json.NewEncoder(w).Encode(&out)
			return
		}
	}

	co, err := mtgmatcher.GetUUID(uuid)
	if err != nil {
		// Try again, assuming it was a scryfall id
		uuid = mtgmatcher.Scryfall2UUID(uuid)
		co, err = mtgmatcher.GetUUID(uuid)
		if err != nil {
			out.Error = "Not found"
			json.NewEncoder(w).Encode(&out)
			return
		}
	}

	out.Data = map[string]*PriceLastSold{}
	out.Identifiers = map[string]string{}

	out.Identifiers["tcgplayer"] = co.Identifiers["tcgplayerProductId"]
	out.Identifiers["cardmarket"] = co.Identifiers["mcmId"]
	out.Identifiers["scryfall"] = co.Identifiers["scryfallId"]
	out.Identifiers["mtgjson"] = uuid

	for _, seller := range Sellers {
		if seller == nil {
			continue
		}
		if SliceStringHas(stores, seller.Info().Shorthand) || req == "*" {
			inv, err := seller.Inventory()
			if err != nil {
				break
			}
			entries := inv[uuid]
			for _, entry := range entries {
				if entry.Conditions == "NM" {
					if out.Data[seller.Info().Shorthand] == nil {
						out.Data[seller.Info().Shorthand] = &PriceLastSold{}
					}
					out.Data[seller.Info().Shorthand].Retail = entry.Price
					break
				}
			}
		}
	}
	for _, vendor := range Vendors {
		if vendor == nil {
			continue
		}
		if SliceStringHas(stores, vendor.Info().Shorthand) || req == "*" {
			bl, err := vendor.Buylist()
			if err != nil {
				break
			}
			entries := bl[uuid]
			for _, entry := range entries {
				if out.Data[vendor.Info().Shorthand] == nil {
					out.Data[vendor.Info().Shorthand] = &PriceLastSold{}
				}
				out.Data[vendor.Info().Shorthand].Buylist = entry.BuyPrice
				break
			}
		}
	}

	json.NewEncoder(w).Encode(&out)
}

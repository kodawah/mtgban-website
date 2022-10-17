package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgmatcher"
	"github.com/kodabb/go-mtgban/tcgplayer"
)

type meta struct {
	Id  int    `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

type ck2id struct {
	Normal *meta `json:"normal,omitempty"`
	Foil   *meta `json:"foil,omitempty"`
	Etched *meta `json:"etched,omitempty"`
}

var CKAPIMutex sync.RWMutex
var CKAPIOutput map[string]*ck2id
var CKAPIData []cardkingdom.CKCard

func CKMirrorAPI(w http.ResponseWriter, r *http.Request) {
	var pricelist struct {
		Data []cardkingdom.CKCard `json:"list"`
	}
	pricelist.Data = CKAPIData

	err := json.NewEncoder(w).Encode(&pricelist)
	if err != nil {
		log.Println(err)
		w.Write([]byte(`{"error": "` + err.Error() + `"}`))
		return
	}
}

func prepareCKAPI() error {
	ServerNotify("api", "CK APIrefresh started")

	list, err := cardkingdom.NewCKClient().GetPriceList()
	if err != nil {
		log.Println(err)
		return err
	}
	CKAPIData = list

	// Backup option for stashing CK data
	rdbRT := ScraperOptions["cardkingdom"].RDBs["retail"]
	rdbBL := ScraperOptions["cardkingdom"].RDBs["buylist"]
	key := time.Now().Format("2006-01-02")

	output := map[string]*ck2id{}

	var skipRedis bool
	for _, card := range list {
		theCard, err := cardkingdom.Preprocess(card)
		if err != nil {
			continue
		}

		cardId, err := mtgmatcher.Match(theCard)
		if err != nil {
			continue
		}

		if card.SellQuantity > 0 && !skipRedis {
			// We use Set for retail because prices are more accurate
			err = rdbRT.HSet(context.Background(), cardId, key, card.SellPrice).Err()
			if err != nil {
				log.Printf("redis error for %s: %s", cardId, err)
				skipRedis = true
			}
		}
		if card.BuyQuantity > 0 && !skipRedis {
			err = rdbBL.HSetNX(context.Background(), cardId, key, card.BuyPrice).Err()
			if err != nil {
				log.Printf("redis error for %s: %s", cardId, err)
				skipRedis = true
			}
		}

		co, err := mtgmatcher.GetUUID(cardId)
		if err != nil {
			continue
		}

		id, found := co.Identifiers["mtgjsonId"]
		if !found {
			id = cardId
		}

		// Allocate memory
		_, found = output[id]
		if !found {
			output[id] = &ck2id{}
		}

		// Set data as needed
		data := &meta{
			Id:  card.Id,
			URL: "https://www.cardkingdom.com/" + card.URL,
		}
		if co.Etched {
			output[id].Etched = data
		} else if co.Foil {
			output[id].Foil = data
		} else {
			output[id].Normal = data
		}
	}

	CKAPIMutex.Lock()
	CKAPIOutput = output
	CKAPIMutex.Unlock()

	ServerNotify("api", "CK API refresh completed")

	return nil
}

func API(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	param := GetParamFromSig(sig, "API")
	canAPI := strings.Contains(param, "CK")
	if SigCheck && !canAPI {
		w.Write([]byte(`{"error": "invalid signature"}`))
		return
	}

	CKAPIMutex.RLock()
	defer CKAPIMutex.RUnlock()
	if CKAPIOutput == nil {
		log.Println("CK API called when list was empty")
		w.Write([]byte(`{"error": "empty list"}`))
		return
	}

	err := json.NewEncoder(w).Encode(CKAPIOutput)
	if err != nil {
		log.Println(err)
		w.Write([]byte(`{"error": "` + err.Error() + `"}`))
		return
	}
}

func getLastSold(cardId string) ([]tcgplayer.LatestSalesData, error) {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return nil, err
	}

	tcgId := co.Identifiers["tcgplayerProductId"]
	if co.Etched {
		id, found := co.Identifiers["tcgplayerEtchedProductId"]
		if found {
			tcgId = id
		}
	}
	if tcgId == "" {
		return nil, errors.New("tcg id not found")
	}

	latestSales, err := tcgplayer.TCGLatestSales(tcgId, co.Foil || co.Etched)
	if err != nil {
		return nil, err
	}

	return latestSales.Data, nil
}

func TCGLastSoldAPI(w http.ResponseWriter, r *http.Request) {
	cardId := strings.TrimPrefix(r.URL.Path, "/api/tcgplayer/lastsold/")
	UserNotify("tcgLastSold", cardId)

	data, err := getLastSold(cardId)
	if err != nil {
		log.Println(err)
		w.Write([]byte(`{"error": "` + err.Error() + `"}`))
		return
	}

	err = json.NewEncoder(w).Encode(data)
	if err != nil {
		log.Println(err)
		w.Write([]byte(`{"error": "` + err.Error() + `"}`))
		return
	}
}

func UUID2CKCSV(w *csv.Writer, ids []string) error {
	var buylist mtgban.BuylistRecord
	for _, vendor := range Vendors {
		if vendor != nil && vendor.Info().Shorthand == "CK" {
			buylist, _ = vendor.Buylist()
			break
		}
	}
	if buylist == nil {
		return errors.New("CK scraper not found")
	}

	header := []string{"Title", "Edition", "Foil", "Quantity"}
	err := w.Write(header)
	if err != nil {
		return err
	}
	for _, id := range ids {
		blEntries, found := buylist[id]
		if !found {
			continue
		}
		name, found := blEntries[0].CustomFields["CKTitle"]
		if !found {
			continue
		}
		edition := blEntries[0].CustomFields["CKEdition"]
		finish := blEntries[0].CustomFields["CKFoil"]
		quantity := fmt.Sprint(1)

		err = w.Write([]string{name, edition, finish, quantity})
		if err != nil {
			return err
		}

		w.Flush()
	}
	return nil
}

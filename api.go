package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/mtgmatcher"
)

type meta struct {
	Id  int    `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

type ck2id struct {
	Normal *meta `json:"normal,omitempty"`
	Foil   *meta `json:"foil,omitempty"`
}

var CKAPIMutex sync.RWMutex
var CKAPIOutput map[string]*ck2id

func prepareCKAPI() error {
	log.Println("Updating CK prices for API users")
	Notify("api", "CK refresh started")

	list, err := cardkingdom.NewCKClient().GetPriceList()
	if err != nil {
		log.Println(err)
		return err
	}

	// Backup option for stashing CK data
	rdbRT := ScraperOptions["cardkingdom"].RDBs["retail"]
	rdbBL := ScraperOptions["cardkingdom"].RDBs["buylist"]
	key := time.Now().Format("2006-01-02")

	output := map[string]*ck2id{}

	for _, card := range list {
		theCard, err := cardkingdom.Preprocess(card)
		if err != nil {
			continue
		}

		cardId, err := mtgmatcher.Match(theCard)
		if err != nil {
			continue
		}

		if card.SellQuantity > 0 {
			// We use Set for retail because prices are more accurate
			err = rdbRT.HSet(context.Background(), cardId, key, card.SellPrice).Err()
			if err != nil {
				log.Printf("redis error for %s: %s", cardId, err)
			}
		}
		if card.BuyQuantity > 0 {
			err = rdbBL.HSetNX(context.Background(), cardId, key, card.BuyPrice).Err()
			if err != nil {
				log.Printf("redis error for %s: %s", cardId, err)
			}
		}

		co, err := mtgmatcher.GetUUID(cardId)
		if err != nil {
			continue
		}

		id := strings.TrimSuffix(cardId, "_f")

		_, found := output[id]
		if !found {
			output[id] = &ck2id{}
		}
		if !co.Foil {
			output[id].Normal = &meta{}
			output[id].Normal.Id = card.Id
			output[id].Normal.URL = "https://www.cardkingdom.com/" + card.URL
		} else {
			output[id].Foil = &meta{}
			output[id].Foil.Id = card.Id
			output[id].Foil.URL = "https://www.cardkingdom.com/" + card.URL
		}
	}

	CKAPIMutex.Lock()
	CKAPIOutput = output
	CKAPIMutex.Unlock()

	log.Println("New CK API output ready")
	Notify("api", "CK refresh completed")

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

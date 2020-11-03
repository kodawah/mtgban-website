package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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

func API(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	param, _ := GetParamFromSig(sig, "API")
	canAPI := strings.Contains(param, "CK")
	if SigCheck && !canAPI {
		http.Error(w, fmt.Sprintf("Invalid signature: %s", param), http.StatusUnauthorized)
		return
	}

	output := map[string]*ck2id{}

	ckclient := cardkingdom.NewCKClient()
	list, err := ckclient.GetPriceList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, card := range list.Data {
		theCard, err := cardkingdom.Preprocess(card)
		if err != nil {
			continue
		}

		cardId, err := mtgmatcher.Match(theCard)
		if err != nil {
			log.Println(err)
			log.Println(theCard)
			log.Println(card)
			alias, ok := err.(*mtgmatcher.AliasingError)
			if ok {
				probes := alias.Probe()
				for _, probe := range probes {
					log.Println("-", probe)
				}
			}
			continue
		}

		co, err := mtgmatcher.GetUUID(cardId)
		if err != nil {
			log.Println(cardId, err)
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

	w.Header().Add("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	err = enc.Encode(output)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	return
}

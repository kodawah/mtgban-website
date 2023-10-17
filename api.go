package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtgban/go-mtgban/cardkingdom"
	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
	"github.com/mtgban/go-mtgban/tcgplayer"

	"golang.org/x/sync/singleflight"
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
	outputMap := CKAPIOutput

	// Only scryfall and mtgjson ids are supported due to the unification of finishes
	// mtgjson is the default, perform a key conversion if scryfall is requested
	idMode := r.FormValue("id")
	if idMode == "scryfall" {
		altMap := map[string]*ck2id{}
		for uuid, meta := range CKAPIOutput {
			co, err := mtgmatcher.GetUUID(uuid)
			if err != nil {
				continue
			}
			id := getIdFunc(idMode)(co)
			altMap[id] = meta
		}
		outputMap = altMap
	}

	err := json.NewEncoder(w).Encode(outputMap)
	if err != nil {
		log.Println(err)
		w.Write([]byte(`{"error": "` + err.Error() + `"}`))
		return
	}
}

var ErrMissingTCGId = errors.New("tcg id not found")

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
		return nil, ErrMissingTCGId
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

var tableGroup singleflight.Group

func RefreshTable(w http.ResponseWriter, r *http.Request) {
	table := filepath.Base(r.URL.Path)

	ServerNotify("tableRefresh", table+" refresh started")

	now := time.Now()

	resp, _, _ := tableGroup.Do(table, func() (interface{}, error) {
		return updateScraper(table), nil
	})

	msg := "ok"
	if resp != nil {
		msg = resp.(error).Error()
		ServerNotify("tableRefresh", msg)
	} else {
		ServerNotify("tableRefresh", fmt.Sprintf("%s refreshed in %v", table, time.Since(now)))
	}
	w.Write([]byte(`{"msg": "` + msg + `"}`))

	doRedir, _ := strconv.ParseBool(r.FormValue("redir"))
	if doRedir {
		http.Redirect(w, r, "/admin?msg="+msg, http.StatusFound)
	}
	return
}

func UUID2CKCSV(w *csv.Writer, ids, qtys []string) error {
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
	for i, id := range ids {
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
		quantity := "1"
		if len(qtys) == len(ids) && qtys[i] != "0" {
			quantity = qtys[i]
		}

		err = w.Write([]string{name, edition, finish, quantity})
		if err != nil {
			return err
		}

		w.Flush()
	}
	return nil
}

func UUID2SCGCSV(w *csv.Writer, ids, qtys []string) error {
	var buylist mtgban.BuylistRecord
	for _, vendor := range Vendors {
		if vendor != nil && vendor.Info().Shorthand == "SCG" {
			buylist, _ = vendor.Buylist()
			break
		}
	}
	if buylist == nil {
		return errors.New("SCG scraper not found")
	}

	header := []string{"name", "set_name", "language", "finish", "quantity"}
	err := w.Write(header)
	if err != nil {
		return err
	}
	for i, id := range ids {
		blEntries, found := buylist[id]
		if !found {
			continue
		}
		name, found := blEntries[0].CustomFields["SCGName"]
		if !found {
			continue
		}
		edition := blEntries[0].CustomFields["SCGEdition"]
		language := blEntries[0].CustomFields["SCGLanguage"]
		finish := blEntries[0].CustomFields["SCGFinish"]
		quantity := "1"
		if len(qtys) == len(ids) && qtys[i] != "0" {
			quantity = qtys[i]
		}

		err = w.Write([]string{name, edition, language, finish, quantity})
		if err != nil {
			return err
		}

		w.Flush()
	}
	return nil
}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	APIVersion = "1"
)

type BanPrice struct {
	Regular float64 `json:"regular,omitempty"`
	Foil    float64 `json:"foil,omitempty"`
}

type PriceAPIOutput struct {
	Error string `json:"error,omitempty"`
	Meta  struct {
		Date     time.Time `json:"date"`
		Version  string    `json:"version"`
		IdFormat string    `json:"id_format,omitempty"`
	} `json:"meta"`

	// uuid > store > price {foil/regular}
	Retail  map[string]map[string]*BanPrice `json:"retail,omitempty"`
	Buylist map[string]map[string]*BanPrice `json:"buylist,omitempty"`
}

func PriceAPI(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")
	out := PriceAPIOutput{}
	out.Meta.Date = time.Now()
	out.Meta.Version = APIVersion

	urlPath := strings.TrimPrefix(r.URL.Path, "/api/mtgban/")

	if !strings.HasSuffix(urlPath, ".json") {
		out.Error = "Not found"
		json.NewEncoder(w).Encode(&out)
		return
	}

	storesOpt := GetParamFromSig(sig, "API")
	var enabledStores []string
	switch storesOpt {
	case "ALL_ACCESS":
		for _, seller := range Sellers {
			if seller != nil && !SliceStringHas(Config.SearchRetailBlockList, seller.Info().Shorthand) {
				enabledStores = append(enabledStores, seller.Info().Shorthand)
			}
		}
		for _, vendor := range Vendors {
			if vendor != nil && !SliceStringHas(Config.SearchBuylistBlockList, vendor.Info().Shorthand) {
				enabledStores = append(enabledStores, vendor.Info().Shorthand)
			}
		}
	case "DEV_ACCESS":
		for _, seller := range Sellers {
			if seller != nil {
				enabledStores = append(enabledStores, seller.Info().Shorthand)
			}
		}
		for _, vendor := range Vendors {
			if vendor != nil {
				enabledStores = append(enabledStores, vendor.Info().Shorthand)
			}
		}
	default:
		enabledStores = strings.Split(storesOpt, ",")
	}
	enabledModes := strings.Split(GetParamFromSig(sig, "APImode"), ",")
	idOpt := r.FormValue("id")
	out.Meta.IdFormat = idOpt

	var doRetail, doBuylist bool
	filterByEdition := ""
	filterByHash := ""
	if strings.Contains(urlPath, "/") {
		base := strings.TrimSuffix(path.Base(urlPath), ".json")

		// Check if the path element is a set name or a hash
		set, err := mtgmatcher.GetSet(base)
		if err == nil {
			filterByEdition = set.Code
		} else {
			_, err = mtgmatcher.GetUUID(base)
			if err != nil {
				// Try again, assuming it was a scryfall id
				uuid := mtgmatcher.Scryfall2UUID(base)
				_, err = mtgmatcher.GetUUID(uuid)
				if err == nil {
					base = uuid
				}
			}
			if err == nil {
				filterByHash = base
			}
		}

		if filterByEdition == "" && filterByHash == "" {
			out.Error = "Not found"
			json.NewEncoder(w).Encode(&out)
			return
		}
	}

	if strings.HasPrefix(urlPath, "retail") {
		doRetail = true
	} else if strings.HasPrefix(urlPath, "buylist") {
		doBuylist = true
	} else if strings.HasPrefix(urlPath, "all") {
		doRetail, doBuylist = true, true
	}

	if doRetail && (SliceStringHas(enabledModes, "retail") || DevMode) {
		out.Retail = getSellerPrices(idOpt, enabledStores, filterByEdition, filterByHash)
	}
	if doBuylist && (SliceStringHas(enabledModes, "buylist") || DevMode) {
		out.Buylist = getVendorPrices(idOpt, enabledStores, filterByEdition, filterByHash)
	}

	if !DevMode {
		user := GetParamFromSig(sig, "UserEmail")
		msg := fmt.Sprintf("%s requested an API dump ('%s','%s')", user, filterByEdition, filterByHash)
		Notify("api", msg)
	}

	if out.Retail == nil && out.Buylist == nil {
		out.Error = "Not found"
		json.NewEncoder(w).Encode(&out)
		return
	}

	json.NewEncoder(w).Encode(&out)
}

func getIdFunc(mode string) func(co *mtgmatcher.CardObject) string {
	switch mode {
	case "tcg":
		return func(co *mtgmatcher.CardObject) string {
			return co.Identifiers["tcgplayerProductId"]
		}
	case "scryfall":
		return func(co *mtgmatcher.CardObject) string {
			return co.Identifiers["scryfallId"]
		}
	case "mtgjson":
		return func(co *mtgmatcher.CardObject) string {
			if co.Foil {
				return strings.TrimSuffix(co.UUID, "_f")
			}
			return co.UUID
		}
	case "mkm":
		return func(co *mtgmatcher.CardObject) string {
			return co.Identifiers["mcmId"]
		}
	case "ck":
		return func(co *mtgmatcher.CardObject) string {
			if co.Foil {
				return co.Identifiers["cardKingdomFoilId"]
			}
			return co.Identifiers["cardKingdomId"]
		}
	}
	return func(co *mtgmatcher.CardObject) string {
		return co.UUID
	}
}

func getSellerPrices(mode string, enabledStores []string, filterByEdition, filterByHash string) map[string]map[string]*BanPrice {
	out := map[string]map[string]*BanPrice{}
	idFunc := getIdFunc(mode)
	for i, seller := range Sellers {
		if seller == nil {
			log.Println("nil seller at position", i)
			continue
		}
		sellerTag := seller.Info().Shorthand

		// Only keep singles
		if seller.Info().SealedMode {
			continue
		}

		// Skip any seller that are not enabled
		if !SliceStringHas(enabledStores, sellerTag) && !DevMode {
			continue
		}

		// Get inventory
		inventory, err := seller.Inventory()
		if err != nil {
			log.Println(err)
			continue
		}

		// Loop through cards
		for cardId := range inventory {
			// No price no dice
			if len(inventory[cardId]) == 0 || inventory[cardId][0].Price == 0 {
				continue
			}

			co, err := mtgmatcher.GetUUID(cardId)
			if err != nil {
				continue
			}

			if filterByEdition != "" && co.SetCode != filterByEdition {
				continue
			} else if filterByHash != "" && cardId != filterByHash {
				continue
			}

			id := idFunc(co)

			_, found := out[id]
			if !found {
				out[id] = map[string]*BanPrice{}
			}
			if out[id][sellerTag] == nil {
				out[id][sellerTag] = &BanPrice{}
			}
			if co.Foil {
				out[id][sellerTag].Foil = inventory[cardId][0].Price
			} else {
				out[id][sellerTag].Regular = inventory[cardId][0].Price
			}
		}
	}

	return out
}

func getVendorPrices(mode string, enabledStores []string, filterByEdition, filterByHash string) map[string]map[string]*BanPrice {
	out := map[string]map[string]*BanPrice{}
	idFunc := getIdFunc(mode)
	for i, vendor := range Vendors {
		if vendor == nil {
			log.Println("nil vendor at position", i)
			continue
		}
		vendorTag := vendor.Info().Shorthand

		// Only keep singles
		if vendor.Info().SealedMode {
			continue
		}

		// Skip any vendor that are not enabled
		if !SliceStringHas(enabledStores, vendorTag) && !DevMode {
			continue
		}

		// Get buylist
		buylist, err := vendor.Buylist()
		if err != nil {
			log.Println(err)
			continue
		}

		// Loop through cards
		for cardId := range buylist {
			// No price no dice
			if len(buylist[cardId]) == 0 || buylist[cardId][0].BuyPrice == 0 {
				continue
			}

			co, err := mtgmatcher.GetUUID(cardId)
			if err != nil {
				continue
			}

			if filterByEdition != "" && co.SetCode != filterByEdition {
				continue
			} else if filterByHash != "" && cardId != filterByHash {
				continue
			}

			id := idFunc(co)

			_, found := out[id]
			if !found {
				out[id] = map[string]*BanPrice{}
			}
			if out[id][vendorTag] == nil {
				out[id][vendorTag] = &BanPrice{}
			}
			if co.Foil {
				out[id][vendorTag].Foil = buylist[cardId][0].BuyPrice
			} else {
				out[id][vendorTag].Regular = buylist[cardId][0].BuyPrice
			}
		}
	}

	return out
}

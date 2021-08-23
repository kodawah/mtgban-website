package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	APIVersion = "1"
)

type BanPrice struct {
	Regular    float64            `json:"regular,omitempty"`
	Foil       float64            `json:"foil,omitempty"`
	Qty        int                `json:"qty,omitempty"`
	QtyFoil    int                `json:"qty_foil,omitempty"`
	Conditions map[string]float64 `json:"conditions,omitempty"`
}

type PriceAPIOutput struct {
	Error string `json:"error,omitempty"`
	Meta  struct {
		Date    time.Time `json:"date"`
		Version string    `json:"version"`
		BaseURL string    `json:"base_url"`
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
	out.Meta.BaseURL = getBaseURL(r) + "/go/"

	urlPath := strings.TrimPrefix(r.URL.Path, "/api/mtgban/")

	if !strings.HasSuffix(urlPath, ".json") {
		out.Error = "Not found"
		json.NewEncoder(w).Encode(&out)
		return
	}

	storesOpt := GetParamFromSig(sig, "API")
	if DevMode && storesOpt == "" {
		storesOpt = "DEV_ACCESS"
	}
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
	qty, _ := strconv.ParseBool(r.FormValue("qty"))
	conds, _ := strconv.ParseBool(r.FormValue("conds"))

	// Filter by user preference, as long as it's listed in the enebled stores
	filterByVendor := r.FormValue("vendor")
	if SliceStringHas(enabledStores, filterByVendor) {
		enabledStores = []string{filterByVendor}
	}

	filterByEdition := ""
	var filterByHash []string
	if strings.Contains(urlPath, "/") {
		base := strings.TrimSuffix(path.Base(urlPath), ".json")

		// Check if the path element is a set name or a hash
		set, err := mtgmatcher.GetSet(base)
		if err == nil {
			filterByEdition = set.Code
		} else {
			co, err := mtgmatcher.GetUUID(base)
			if err != nil {
				// Try again, assuming it was a scryfall id, fallback to tcg id
				uuid := mtgmatcher.Scryfall2UUID(base)
				if uuid == "" {
					uuid = mtgmatcher.Tcg2UUID(base)
				}
				co, err = mtgmatcher.GetUUID(uuid)
				if err == nil {
					base = uuid
				}
			}
			if err == nil {
				filterByHash = append(filterByHash, base)

				// Check if there is a foil (or nonfoil) version of the card
				altId, err := mtgmatcher.Match(&mtgmatcher.Card{
					Id:   base,
					Foil: !co.Foil,
				})
				if err == nil && altId != base {
					filterByHash = append(filterByHash, altId)
				}

				// Speed up search by keeping only the needed edition
				filterByEdition = co.SetCode
			}
		}

		if filterByEdition == "" && filterByHash == nil {
			out.Error = "Not found"
			json.NewEncoder(w).Encode(&out)
			return
		}
	}

	dumpType := ""
	canRetail := SliceStringHas(enabledModes, "retail") || (SliceStringHas(enabledModes, "all") || (DevMode && !SigCheck))
	canBuylist := SliceStringHas(enabledModes, "buylist") || (SliceStringHas(enabledModes, "all") || (DevMode && !SigCheck))
	if (strings.HasPrefix(urlPath, "retail") || strings.HasPrefix(urlPath, "all")) && canRetail {
		dumpType += "retail"
		out.Retail = getSellerPrices(idOpt, enabledStores, filterByEdition, filterByHash, qty, conds)
	}
	if (strings.HasPrefix(urlPath, "buylist") || strings.HasPrefix(urlPath, "all")) && canBuylist {
		dumpType += "buylist"
		out.Buylist = getVendorPrices(idOpt, enabledStores, filterByEdition, filterByHash, qty, conds)
	}

	user := GetParamFromSig(sig, "UserEmail")
	msg := fmt.Sprintf("%s requested a '%s' API dump ('%s','%q')", user, dumpType, filterByEdition, filterByHash)
	if qty {
		msg += " with quantities"
	}

	if DevMode {
		log.Println(msg)
	} else {
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

func getSellerPrices(mode string, enabledStores []string, filterByEdition string, filterByHash []string, qty bool, conds bool) map[string]map[string]*BanPrice {
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
		if !SliceStringHas(enabledStores, sellerTag) {
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
			}
			if filterByHash != nil && !SliceStringHas(filterByHash, cardId) {
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

			// Determine whether the response should include qty information
			// Needs to be explicitly requested, all the index prices are skipped,
			// TCG is too due to how quantities are stored in mtgban (FIXME?)
			// (only for retail).
			shouldQty := qty && !seller.Info().MetadataOnly && sellerTag != "TCG Player" && sellerTag != "TCG Direct"

			if co.Foil {
				out[id][sellerTag].Foil = inventory[cardId][0].Price
				if shouldQty {
					for i := range inventory[cardId] {
						out[id][sellerTag].QtyFoil += inventory[cardId][i].Quantity
					}
				} else if len(enabledStores) == 1 || (filterByHash != nil && conds) {
					if out[id][sellerTag].Conditions == nil {
						out[id][sellerTag].Conditions = map[string]float64{}
					}
					for i := range inventory[cardId] {
						condTag := inventory[cardId][i].Conditions
						out[id][sellerTag].Conditions[condTag+"_foil"] = inventory[cardId][i].Price
					}
				}
			} else {
				out[id][sellerTag].Regular = inventory[cardId][0].Price
				if shouldQty {
					for i := range inventory[cardId] {
						out[id][sellerTag].Qty += inventory[cardId][i].Quantity
					}
				} else if len(enabledStores) == 1 || (filterByHash != nil && conds) {
					if out[id][sellerTag].Conditions == nil {
						out[id][sellerTag].Conditions = map[string]float64{}
					}
					for i := range inventory[cardId] {
						out[id][sellerTag].Conditions[inventory[cardId][i].Conditions] = inventory[cardId][i].Price
					}
				}
			}
		}
	}

	return out
}

func getVendorPrices(mode string, enabledStores []string, filterByEdition string, filterByHash []string, qty bool, conds bool) map[string]map[string]*BanPrice {
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
			}
			if filterByHash != nil && !SliceStringHas(filterByHash, cardId) {
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
				if qty && !vendor.Info().MetadataOnly {
					for i := range buylist[cardId] {
						out[id][vendorTag].QtyFoil += buylist[cardId][i].Quantity
					}
				} else if len(enabledStores) == 1 || (filterByHash != nil && conds) {
					if out[id][vendorTag].Conditions == nil {
						out[id][vendorTag].Conditions = map[string]float64{}
					}
					for i := range buylist[cardId] {
						condTag := buylist[cardId][i].Conditions
						out[id][vendorTag].Conditions[condTag+"_foil"] = buylist[cardId][i].BuyPrice
					}
				}
			} else {
				out[id][vendorTag].Regular = buylist[cardId][0].BuyPrice
				if qty && !vendor.Info().MetadataOnly {
					for i := range buylist[cardId] {
						out[id][vendorTag].Qty += buylist[cardId][i].Quantity
					}
				} else if len(enabledStores) == 1 || (filterByHash != nil && conds) {
					if out[id][vendorTag].Conditions == nil {
						out[id][vendorTag].Conditions = map[string]float64{}
					}
					for i := range buylist[cardId] {
						out[id][vendorTag].Conditions[buylist[cardId][i].Conditions] = buylist[cardId][i].BuyPrice
					}
				}
			}
		}
	}

	return out
}

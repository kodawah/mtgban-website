package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

const (
	MaxArbitResults = 600
	MaxPriceRatio   = 120.0
	MaxSpread       = 650.0
	MinSpread       = 10.0
	MaxSpreadGlobal = 1000
	MinSpreadGlobal = 200.0

	MaxResultsGlobal      = 300
	MaxResultsGlobalLimit = 50

	MinSpreadNegative = -30
	MinDiffNegative   = -100

	MinSpreadHighYield       = 100
	MinSpreadHighYieldGlobal = 350
)

var FilteredEditions = []string{
	"Collectors’ Edition",
	"Foreign Black Border",
	"Foreign White Border",
	"Intl. Collectors’ Edition",
	"Limited Edition Alpha",
	"Limited Edition Beta",
	"Unlimited Edition",
	"Legends Italian",
	"The Dark Italian",
	"Rinascimento",
	"Chronicles Japanese",
	"Foreign Black Border",
	"Fourth Edition Black Border",
}

// Every single boolean option
var FilterOptKeys = []string{
	"credit",
	"nocond",
	"nofoil",
	"nocomm",
	"nononrl",
	"noposi",
	"nopenny",
	"nobuypenny",
	"nolow",
	"nodiff",
	"noqty",
}

// User-readable option name (may be a subset of the options)
var FilterOptNames = map[string]string{
	"nocond":     "only NM/SP",
	"nofoil":     "only non-Foil",
	"nocomm":     "only Rare/Mythic",
	"nononrl":    "only RL",
	"noposi":     "only Negative",
	"nopenny":    "only Bucks+",
	"nobuypenny": "only BuyBucks+",
	"nolow":      "only Yield+",
	"nodiff":     "only Difference+",
	"noqty":      "only Quantity+",
}

// Arbit-only options
var FilteOptNoGlobal = map[string]bool{
	"nocond":     true,
	"nobuypenny": true,
	"noposi":     true,
	"noqty":      true,
}

var BadConditions = []string{"MP", "HP", "PO"}
var UCRarity = []string{"uncommon", "common"}

type Arbitrage struct {
	Name       string
	LastUpdate string
	Arbit      []mtgban.ArbitEntry
	HasCredit  bool

	HasConditions bool
}

func Arbit(w http.ResponseWriter, r *http.Request) {
	arbit(w, r, false)
}

func Reverse(w http.ResponseWriter, r *http.Request) {
	arbit(w, r, true)
}

func arbit(w http.ResponseWriter, r *http.Request, reverse bool) {
	sig := getSignatureFromCookies(r)

	pageName := "Arbitrage"
	if reverse {
		pageName = "Reverse"
	}
	pageVars := genPageNav(pageName, sig)

	var allowlistSellers []string
	allowlistSellersOpt := GetParamFromSig(sig, "ArbitEnabled")

	if allowlistSellersOpt == "ALL" {
		for _, seller := range Sellers {
			if seller == nil || seller.Info().SealedMode {
				continue
			}
			allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
		}
	} else if allowlistSellersOpt == "DEV" {
		allowlistSellers = append(Config.ArbitDefaultSellers, Config.DevSellers...)
	} else if allowlistSellersOpt == "DEFAULT" || allowlistSellersOpt == "" {
		allowlistSellers = Config.ArbitDefaultSellers
	} else {
		allowlistSellers = strings.Split(allowlistSellersOpt, ",")
	}

	var blocklistVendors []string
	blocklistVendorsOpt := GetParamFromSig(sig, "ArbitDisabledVendors")
	if blocklistVendorsOpt == "DEFAULT" || blocklistVendorsOpt == "" {
		blocklistVendors = Config.ArbitBlockVendors
	} else if blocklistVendorsOpt != "NONE" {
		blocklistVendors = strings.Split(blocklistVendorsOpt, ",")
	}

	if r.FormValue("page") == "opt" {
		// Load all available vendors
		vendorKeys := make([]string, 0, len(blocklistVendors))
		for _, vendor := range Vendors {
			if vendor == nil || SliceStringHas(blocklistVendors, vendor.Info().Shorthand) || vendor.Info().SealedMode {
				continue
			}
			vendorKeys = append(vendorKeys, vendor.Info().Shorthand)
		}
		sort.Slice(vendorKeys, func(i, j int) bool {
			return ScraperNames[vendorKeys[i]] < ScraperNames[vendorKeys[j]]
		})
		pageVars.VendorKeys = vendorKeys
	} else {
		filters := strings.Split(readCookie(r, "ArbitVendorsList"), ",")
		for _, code := range filters {
			if !SliceStringHas(blocklistVendors, code) {
				blocklistVendors = append(blocklistVendors, code)
			}
		}
	}

	pageVars.ReverseMode = reverse

	scraperCompare(w, r, pageVars, allowlistSellers, blocklistVendors)
}

func Global(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Global", sig)

	anyEnabledOpt := GetParamFromSig(sig, "AnyEnabled")
	anyEnabled, _ := strconv.ParseBool(anyEnabledOpt)

	anyExperimentOpt := GetParamFromSig(sig, "AnyExperimentsEnabled")
	anyExperiment, _ := strconv.ParseBool(anyExperimentOpt)

	anyEnabled = anyEnabled || (DevMode && !SigCheck)
	anyExperiment = anyExperiment || (DevMode && !SigCheck)

	// The "menu" section, the reference
	var allowlistSellers []string
	for i, seller := range Sellers {
		if seller == nil {
			log.Println("nil seller at position", i)
			continue
		}
		if anyEnabled {
			// This is the list of allowed global sellers, minus the ones blocked from search
			if SliceStringHas(Config.GlobalAllowList, seller.Info().Shorthand) {
				if !anyExperiment && SliceStringHas(Config.SearchRetailBlockList, seller.Info().Shorthand) {
					continue
				}
				allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
			} else if anyExperiment && SliceStringHas(Config.DevSellers, seller.Info().Shorthand) {
				// Append any experimental ones if enabled
				allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
			}
		} else {
			// These are hardcoded to provide a preview of the tool
			if seller.Info().Shorthand != TCG_MARKET &&
				seller.Info().Shorthand != MKM_TREND {
				continue
			}
			allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
		}
	}

	// The "Jump to" section, the probe
	var blocklistVendors []string
	for i, seller := range Sellers {
		if seller == nil {
			log.Println("nil seller at position", i)
			continue
		}
		if SliceStringHas(Config.GlobalProbeList, seller.Info().Shorthand) {
			continue
		}
		blocklistVendors = append(blocklistVendors, seller.Info().Shorthand)
	}

	// Inform the render this is Global
	pageVars.GlobalMode = true

	scraperCompare(w, r, pageVars, allowlistSellers, blocklistVendors, anyEnabled)
}

func scraperCompare(w http.ResponseWriter, r *http.Request, pageVars PageVars, allowlistSellers []string, blocklistVendors []string, flags ...bool) {
	r.ParseForm()

	var source mtgban.Scraper
	var message string
	var sorting string
	arbitFilters := map[string]bool{}

	// Set these flags for global, since it's likely users will want them
	if pageVars.GlobalMode {
		arbitFilters["nopenny"] = !arbitFilters["nopenny"]
		arbitFilters["nodiff"] = !arbitFilters["nodiff"]
	}

	for k, v := range r.Form {
		switch k {
		case "source":
			// Source can be a Seller or Vendor depending on operation mode
			if pageVars.ReverseMode {
				if SliceStringHas(blocklistVendors, v[0]) {
					log.Println("Unauthorized attempt with", v[0])
					message = "Unknown " + v[0] + " seller"
					break
				}

				for i, vendor := range Vendors {
					if vendor == nil {
						log.Println("nil vendor at position", i)
						continue
					}
					if vendor.Info().Shorthand == v[0] {
						source = vendor
						break
					}
				}
			} else {
				if !SliceStringHas(allowlistSellers, v[0]) {
					log.Println("Unauthorized attempt with", v[0])
					message = "Unknown " + v[0] + " seller"
					break
				}

				for i, seller := range Sellers {
					if seller == nil {
						log.Println("nil seller at position", i)
						continue
					}
					if seller.Info().Shorthand == v[0] {
						source = seller
						break
					}
				}
			}
			if source == nil {
				message = "Unknown " + v[0] + " source"
			}

		case "sort":
			sorting = v[0]

		// Assume anything else is a boolean option
		default:
			arbitFilters[k], _ = strconv.ParseBool(v[0])
		}
	}

	if message != "" {
		pageVars.Title = "Errors have been made"
		pageVars.ErrorMessage = message

		render(w, "arbit.html", pageVars)
		return
	}

	// Set up menu bar, by selecting which scrapers should be selectable as source
	var menuScrapers []mtgban.Scraper
	if pageVars.ReverseMode {
		for _, vendor := range Vendors {
			if vendor == nil || SliceStringHas(blocklistVendors, vendor.Info().Shorthand) {
				continue
			}
			menuScrapers = append(menuScrapers, vendor)
		}
	} else {
		for _, seller := range Sellers {
			if seller == nil || !SliceStringHas(allowlistSellers, seller.Info().Shorthand) {
				continue
			}
			menuScrapers = append(menuScrapers, seller)
		}
	}

	// Populate the menu bar with the pool selected above
	for _, scraper := range menuScrapers {
		var link string
		if pageVars.GlobalMode {
			link = "/global"
		} else {
			if scraper.Info().MetadataOnly {
				continue
			}
			link = "/arbit"
			if pageVars.ReverseMode {
				link = "/reverse"
			}
		}

		nav := NavElem{
			Name:  scraper.Info().Name,
			Short: scraper.Info().Shorthand,
			Link:  link,
		}

		if scraper.Info().SealedMode {
			nav.Name += " Sealed"
		}

		v := url.Values{}
		v.Set("source", scraper.Info().Shorthand)
		for key, val := range arbitFilters {
			v.Set(key, fmt.Sprint(val))
		}
		v.Set("sort", fmt.Sprint(sorting))

		nav.Link += "?" + v.Encode()

		if source != nil && source.Info().Shorthand == scraper.Info().Shorthand {
			nav.Active = true
			nav.Class = "selected"
		}
		pageVars.ExtraNav = append(pageVars.ExtraNav, nav)
	}

	if source == nil {
		if len(flags) > 0 && !flags[0] {
			pageVars.InfoMessage = "Increase your tier to discover more cards and more markets!"
		}

		render(w, "arbit.html", pageVars)
		return
	}

	pageVars.ScraperShort = source.Info().Shorthand
	pageVars.HasAffiliate = SliceStringHas(Config.AffiliatesList, source.Info().Shorthand)
	pageVars.QtyNotAvailable = source.Info().NoQuantityInventory
	pageVars.ArbitFilters = arbitFilters
	pageVars.ArbitOptKeys = FilterOptKeys
	pageVars.ArbitOptNames = FilterOptNames
	pageVars.ArbitOptNoGlob = FilteOptNoGlobal

	pageVars.Arb = []Arbitrage{}
	pageVars.Metadata = map[string]GenericCard{}

	opts := &mtgban.ArbitOpts{
		MinSpread:       MinSpread,
		MaxSpread:       MaxSpread,
		MaxPriceRatio:   MaxPriceRatio,
		NoFoil:          arbitFilters["nofoil"],
		OnlyReserveList: arbitFilters["nononrl"],
	}
	if pageVars.GlobalMode {
		opts.MinSpread = MinSpreadGlobal
		opts.MaxSpread = MaxSpreadGlobal

		if source.Info().Shorthand == TCG_DIRECT {
			opts.Conditions = BadConditions
		}
	}
	if arbitFilters["noposi"] {
		opts.MinSpread = MinSpreadNegative
		opts.MinDiff = MinDiffNegative
		opts.MaxSpread = MinSpread
	}
	if arbitFilters["nolow"] {
		opts.MinSpread = MinSpreadHighYield
		if pageVars.GlobalMode {
			opts.MinSpread = MinSpreadHighYieldGlobal
		}
	}
	if arbitFilters["nocond"] {
		opts.Conditions = BadConditions
	}
	if arbitFilters["nocomm"] {
		opts.Rarities = UCRarity
	}
	if arbitFilters["nopenny"] {
		opts.MinPrice = 1
	}
	if arbitFilters["nobuypenny"] {
		opts.MinBuyPrice = 1
	}
	if arbitFilters["nodiff"] {
		opts.MinDiff = 1
		if pageVars.GlobalMode {
			opts.MinDiff = 5
		}
	}
	if arbitFilters["noqty"] {
		opts.MinQuantity = 1
	}
	if pageVars.GlobalMode {
		opts.Editions = FilteredEditions
	}

	// The pool of scrapers that source will be compared against
	var scrapers []mtgban.Scraper
	if pageVars.GlobalMode || pageVars.ReverseMode {
		for i, seller := range Sellers {
			if seller == nil {
				log.Println("nil seller at position", i)
				continue
			}
			scrapers = append(scrapers, seller)
		}
	} else {
		for i, vendor := range Vendors {
			if vendor == nil {
				log.Println("nil vendor at position", i)
				continue
			}
			scrapers = append(scrapers, vendor)
		}
	}

	for _, scraper := range scrapers {
		if scraper.Info().Shorthand == source.Info().Shorthand {
			continue
		}
		if pageVars.ReverseMode {
			if scraper.Info().MetadataOnly {
				continue
			}
		} else {
			if SliceStringHas(blocklistVendors, scraper.Info().Shorthand) {
				continue
			}
		}

		if scraper.Info().Shorthand == "ABU" {
			opts.UseTrades = arbitFilters["credit"]
		}

		var arbit []mtgban.ArbitEntry
		var err error
		if pageVars.GlobalMode {
			arbit, err = mtgban.Mismatch(opts, scraper.(mtgban.Seller), source.(mtgban.Seller))
		} else if pageVars.ReverseMode {
			arbit, err = mtgban.Arbit(opts, source.(mtgban.Vendor), scraper.(mtgban.Seller))
		} else {
			arbit, err = mtgban.Arbit(opts, scraper.(mtgban.Vendor), source.(mtgban.Seller))
		}
		if err != nil {
			log.Println(err)
			continue
		}

		if len(arbit) == 0 {
			continue
		}

		// For Global, drop results before sorting, to add some extra variance
		if pageVars.GlobalMode {
			maxResults := MaxResultsGlobal
			// Lower max number of results for the preview
			if len(flags) > 0 && !flags[0] {
				maxResults = MaxResultsGlobalLimit
			}
			if len(arbit) > maxResults {
				arbit = arbit[:maxResults]
			}
		}

		// Sort as requested
		switch sorting {
		case "available":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].InventoryEntry.Quantity > arbit[j].InventoryEntry.Quantity
			})
		case "sell_price":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].InventoryEntry.Price > arbit[j].InventoryEntry.Price
			})
		case "buy_price":
			if pageVars.GlobalMode {
				sort.Slice(arbit, func(i, j int) bool {
					return arbit[i].ReferenceEntry.Price > arbit[j].ReferenceEntry.Price
				})
			} else {
				sort.Slice(arbit, func(i, j int) bool {
					return arbit[i].BuylistEntry.BuyPrice > arbit[j].BuylistEntry.BuyPrice
				})
			}
		case "trade_price":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].BuylistEntry.TradePrice > arbit[j].BuylistEntry.TradePrice
			})
		case "diff":
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].Difference > arbit[j].Difference
			})
		default:
			sort.Slice(arbit, func(i, j int) bool {
				return arbit[i].Spread > arbit[j].Spread
			})
		}
		pageVars.SortOption = sorting

		// For Arbit, drop any excessive results after sorting
		if !pageVars.GlobalMode && len(arbit) > MaxArbitResults {
			arbit = arbit[:MaxArbitResults]
		}

		name := scraper.Info().Name
		if name == "TCG Player Market" {
			name = "TCG Player Trade-In"
		}

		entry := Arbitrage{
			Name:       name,
			LastUpdate: scraper.Info().BuylistTimestamp.Format(time.RFC3339),
			Arbit:      arbit,
			HasCredit:  !scraper.Info().NoCredit,
		}
		if pageVars.GlobalMode {
			entry.HasCredit = false
			entry.LastUpdate = scraper.Info().InventoryTimestamp.Format(time.RFC3339)
			entry.HasConditions = source.Info().MetadataOnly
		}

		pageVars.Arb = append(pageVars.Arb, entry)
		for i := range arbit {
			cardId := arbit[i].CardId
			_, found := pageVars.Metadata[cardId]
			if found {
				continue
			}
			pageVars.Metadata[cardId] = uuid2card(cardId, true)
			if pageVars.Metadata[cardId].Reserved {
				pageVars.HasReserved = true
			}
			if pageVars.Metadata[cardId].Stocks {
				pageVars.HasStocks = true
			}
		}
	}

	if len(pageVars.Arb) == 0 {
		pageVars.InfoMessage = "No arbitrage available!"
	}

	if pageVars.GlobalMode {
		pageVars.Title = "Market Imbalance in " + source.Info().Name
	} else {
		pageVars.Title = "Arbitrage"
		if pageVars.ReverseMode {
			pageVars.Title += " towards "
		} else {
			pageVars.Title += " from "
		}
		pageVars.Title += source.Info().Name
	}

	render(w, "arbit.html", pageVars)
}

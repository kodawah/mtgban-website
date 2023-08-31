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

	"github.com/mtgban/go-mtgban/mtgban"
	"golang.org/x/exp/slices"
)

const (
	MaxArbitResults = 450
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
	"onlyfoil",
	"nocomm",
	"nononrl",
	"nononabu4h",
	"onlyshiny",
	"noposi",
	"nopenny",
	"nobuypenny",
	"nolow",
	"nodiff",
	"nodiffplus",
	"noqty",
}

type FilterOpt struct {
	Title string
	Func  func(*mtgban.ArbitOpts)
}

// User-readable option name (may be a subset of the options)
var FilterOptConfig = map[string]FilterOpt{
	"nocond": {
		Title: "only NM/SP",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.Conditions = BadConditions
		},
	},
	"nofoil": {
		Title: "only non-Foil",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.NoFoil = true
		},
	},
	"onlyfoil": {
		Title: "only Foil",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.OnlyFoil = true
		},
	},
	"nocomm": {
		Title: "only Rare/Mythic",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.Rarities = UCRarity
		},
	},
	"nononrl": {
		Title: "only RL",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.OnlyReserveList = true
		},
	},
	"nononabu4h": {
		Title: "only ABU4H",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.OnlyEditions = ABU4H
		},
	},
	"onlyshiny": {
		Title: "only Shinies",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.OnlyEditions = ShinyEditions
			opts.OnlyCollectorNumberRanges = ShinyEditionRanges
		},
	},
	"noposi": {
		Title: "only Negative",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinSpread = MinSpreadNegative
			opts.MinDiff = MinDiffNegative
			opts.MaxSpread = MinSpread
		},
	},
	"nopenny": {
		Title: "only Bucks+",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinPrice = 1
		},
	},
	"nobuypenny": {
		Title: "only BuyBucks+",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinBuyPrice = 1
		},
	},
	"nolow": {
		Title: "only Yield+",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinSpread = MinSpreadHighYield
		},
	},
	"nodiff": {
		Title: "only Difference+",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinDiff = 1
		},
	},
	"nodiffplus": {
		Title: "only Difference++",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinDiff = 5
		},
	},
	"noqty": {
		Title: "only Quantity+",
		Func: func(opts *mtgban.ArbitOpts) {
			opts.MinQuantity = 1
		},
	},
}

// Arbit-only options
var FilterOptNoGlobal = map[string]bool{
	"nononabu4h": true,
	"nobuypenny": true,
	"noposi":     true,
	"noqty":      true,
}

// Experimental options
var FilterOptTests = map[string]bool{
	"nononrl":    true,
	"nononabu4h": true,
	"onlyshiny":  true,
}

var BadConditions = []string{"MP", "HP", "PO"}
var UCRarity = []string{"uncommon", "common"}

var ABU4H = []string{
	"Limited Edition Alpha",
	"Limited Edition Beta",
	"Unlimited Edition",
	"Arabian Nights",
	"Antiquities",
	"Legends",
	"The Dark",
}

var ShinyEditions = []string{
	"Seventh Edition",
	"Zendikar Expeditions",
	"Kaladesh Inventions",
	"Amonkhet Invocations",
	"Mythic Edition",
	"Ultimate Box Topper",
	"Secret Lair Drop",
	"Zendikar Rising Expeditions",
	"Modern Horizons 1 Timeshifts",

	// Filtered below
	"Ikoria: Lair of Behemoths",
	"Commander Legends",
	"Double Masters",
	"Time Spiral Remastered",
	"Strixhaven Mystical Archive",
}

var ShinyEditionRanges = map[string][2]int{
	// Godzilla series
	"Ikoria: Lair of Behemoths": {370, 387},
	// Etched commanders
	"Commander Legends": {514, 614},
	// Box toppers
	"Double Masters": {333, 372},
	// Timeshifts
	"Time Spiral Remastered": {290, 411},
	// JPN cards
	"Strixhaven Mystical Archive": {64, 126},
}

type Arbitrage struct {
	Name      string
	Arbit     []mtgban.ArbitEntry
	HasCredit bool

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

	var anyOptionEnabled bool

	var allowlistSellers []string
	allowlistSellersOpt := GetParamFromSig(sig, "ArbitEnabled")

	if allowlistSellersOpt == "ALL" || (DevMode && !SigCheck) {
		for _, seller := range Sellers {
			if seller == nil || seller.Info().SealedMode || seller.Info().MetadataOnly {
				continue
			}
			allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
		}
		// Enable any option under FilterOptTests
		anyOptionEnabled = true
	} else if allowlistSellersOpt == "DEV" {
		allowlistSellers = append(Config.ArbitDefaultSellers, Config.DevSellers...)
	} else if allowlistSellersOpt == "" {
		allowlistSellers = Config.ArbitDefaultSellers
	} else {
		allowlistSellers = strings.Split(allowlistSellersOpt, ",")
	}

	var blocklistVendors []string
	blocklistVendorsOpt := GetParamFromSig(sig, "ArbitDisabledVendors")
	if blocklistVendorsOpt == "" {
		blocklistVendors = Config.ArbitBlockVendors
	} else if blocklistVendorsOpt != "NONE" {
		blocklistVendors = strings.Split(blocklistVendorsOpt, ",")
	}

	if r.FormValue("page") == "opt" {
		// Load all available vendors
		vendorKeys := make([]string, 0, len(blocklistVendors))
		for _, vendor := range Vendors {
			if vendor == nil || slices.Contains(blocklistVendors, vendor.Info().Shorthand) || vendor.Info().SealedMode {
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
			if !slices.Contains(blocklistVendors, code) {
				blocklistVendors = append(blocklistVendors, code)
			}
		}
	}

	pageVars.ReverseMode = reverse

	start := time.Now()

	scraperCompare(w, r, pageVars, allowlistSellers, blocklistVendors, true, anyOptionEnabled)

	user := GetParamFromSig(sig, "UserEmail")
	msg := fmt.Sprintf("Request by %s took %v", user, time.Since(start))
	UserNotify("arbit", msg)
	LogPages["Arbit"].Println(msg)
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
	for _, seller := range Sellers {
		if seller == nil {
			continue
		}
		if anyEnabled {
			// This is the list of allowed global sellers, minus the ones blocked from search
			if slices.Contains(Config.GlobalAllowList, seller.Info().Shorthand) {
				if !anyExperiment && slices.Contains(Config.SearchRetailBlockList, seller.Info().Shorthand) {
					continue
				}
				allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
			} else if anyExperiment && slices.Contains(Config.DevSellers, seller.Info().Shorthand) {
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
	for _, seller := range Sellers {
		if seller == nil {
			continue
		}
		if slices.Contains(Config.GlobalProbeList, seller.Info().Shorthand) {
			continue
		}
		blocklistVendors = append(blocklistVendors, seller.Info().Shorthand)
	}

	// Inform the render this is Global
	pageVars.GlobalMode = true

	start := time.Now()

	scraperCompare(w, r, pageVars, allowlistSellers, blocklistVendors, anyEnabled)

	user := GetParamFromSig(sig, "UserEmail")
	msg := fmt.Sprintf("Request by %s took %v", user, time.Since(start))
	UserNotify("global", msg)
	LogPages["Global"].Println(msg)
}

func scraperCompare(w http.ResponseWriter, r *http.Request, pageVars PageVars, allowlistSellers []string, blocklistVendors []string, flags ...bool) {
	r.ParseForm()

	var source mtgban.Scraper
	var message string
	var sorting string
	arbitFilters := map[string]bool{}

	limitedResults := len(flags) > 0 && !flags[0]
	anyOptionEnabled := len(flags) > 1 && flags[1]

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
				if slices.Contains(blocklistVendors, v[0]) {
					log.Println("Unauthorized attempt with", v[0])
					message = "Unknown " + v[0] + " seller"
					break
				}

				for _, vendor := range Vendors {
					if vendor == nil {
						continue
					}
					if vendor.Info().Shorthand == v[0] {
						source = vendor
						break
					}
				}
			} else {
				if !slices.Contains(allowlistSellers, v[0]) {
					log.Println("Unauthorized attempt with", v[0])
					message = "Unknown " + v[0] + " seller"
					break
				}

				for _, seller := range Sellers {
					if seller == nil {
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
			// Skip options reserved for arbit-only
			if pageVars.GlobalMode && FilterOptNoGlobal[k] {
				continue
			}
			// Skip experimental options
			if !anyOptionEnabled && FilterOptTests[k] {
				continue
			}
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
			if vendor == nil || slices.Contains(blocklistVendors, vendor.Info().Shorthand) {
				continue
			}
			menuScrapers = append(menuScrapers, vendor)
		}
	} else {
		for _, seller := range Sellers {
			if seller == nil || !slices.Contains(allowlistSellers, seller.Info().Shorthand) {
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
		if limitedResults {
			pageVars.InfoMessage = "Increase your tier to discover more cards and more markets!"
		}

		render(w, "arbit.html", pageVars)
		return
	}

	pageVars.ScraperShort = source.Info().Shorthand
	pageVars.HasAffiliate = slices.Contains(Config.AffiliatesList, source.Info().Shorthand)
	pageVars.QtyNotAvailable = source.Info().NoQuantityInventory
	pageVars.ArbitFilters = arbitFilters
	pageVars.ArbitOptKeys = FilterOptKeys
	pageVars.ArbitOptConfig = FilterOptConfig
	pageVars.ArbitOptNoGlob = FilterOptNoGlobal
	if !anyOptionEnabled {
		pageVars.ArbitOptTests = FilterOptTests
	}

	pageVars.Arb = []Arbitrage{}
	pageVars.Metadata = map[string]GenericCard{}

	opts := &mtgban.ArbitOpts{
		MinSpread:     MinSpread,
		MaxSpread:     MaxSpread,
		MaxPriceRatio: MaxPriceRatio,
	}

	// Set options
	for _, key := range FilterOptKeys {
		isSet := arbitFilters[key]
		_, hasFunc := FilterOptConfig[key]
		if isSet && hasFunc {
			FilterOptConfig[key].Func(opts)
		}
	}

	// Customize opts for Globals
	if pageVars.GlobalMode {
		opts.MinSpread = MinSpreadGlobal
		opts.MaxSpread = MaxSpreadGlobal

		if arbitFilters["nolow"] {
			opts.MinSpread = MinSpreadHighYieldGlobal
		}
		if arbitFilters["nodiff"] {
			opts.MinDiff = 5
		}
		if arbitFilters["nodiffplus"] {
			opts.MinDiff = 10
		}

		opts.Editions = FilteredEditions
	}

	// The pool of scrapers that source will be compared against
	var scrapers []mtgban.Scraper
	if pageVars.GlobalMode || pageVars.ReverseMode {
		for _, seller := range Sellers {
			if seller == nil {
				continue
			}
			scrapers = append(scrapers, seller)
		}
	} else {
		for _, vendor := range Vendors {
			if vendor == nil {
				continue
			}
			scrapers = append(scrapers, vendor)
		}
	}

	for _, scraper := range scrapers {
		if scraper.Info().Shorthand == source.Info().Shorthand {
			continue
		}
		if !pageVars.ReverseMode {
			if slices.Contains(blocklistVendors, scraper.Info().Shorthand) {
				continue
			}
		}

		// Set custom scraper options
		if pageVars.GlobalMode && scraper.Info().Shorthand == TCG_DIRECT {
			opts.Conditions = BadConditions
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
			if limitedResults {
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
			Name:      name,
			Arbit:     arbit,
			HasCredit: !scraper.Info().NoCredit,
		}
		if pageVars.GlobalMode {
			entry.HasCredit = false
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
			if pageVars.Metadata[cardId].SypList {
				pageVars.HasSypList = true
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

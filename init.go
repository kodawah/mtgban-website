package main

import (
	"log"
	"os"
	"sync"

	"github.com/go-redis/redis/v8"

	"github.com/mtgban/go-mtgban/mtgstocks"
	"github.com/mtgban/go-mtgban/tcgplayer"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
)

const (
	// from TCGIndex
	TCG_LOW        = "TCG Low"
	TCG_MARKET     = "TCG Market"
	TCG_DIRECT_LOW = "TCG Direct Low"

	// from TCGMrkt
	TCG_MAIN    = "TCG Player"
	TCG_DIRECT  = "TCG Direct"
	TCG_BUYLIST = "TCG Player Market"

	// from TCGDirectNet
	TCG_DIRECT_NET = "TCG Direct (net)"

	// from MKMIndex
	MKM_LOW   = "MKM Low"
	MKM_TREND = "MKM Trend"

	// from CT
	CT_STANDARD        = "Card Trader"
	CT_ZERO            = "Card Trader Zero"
	CT_STANDARD_SEALED = "Card Trader Sealed"
	CT_ZERO_SEALED     = "Card Trader Zero Sealed"

	AllPrintingsFileName = "allprintings5.json"
)

func loadDatastore() error {
	allPrintingsReader, err := os.Open(AllPrintingsFileName)
	if err != nil {
		return err
	}
	defer allPrintingsReader.Close()

	return mtgmatcher.LoadDatastore(allPrintingsReader)
}

func loadSellerFromFile(fname string) (mtgban.Seller, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return mtgban.ReadSellerFromJSON(file)
}

func dumpSellerToFile(seller mtgban.Seller, fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer file.Close()

	return mtgban.WriteSellerToJSON(seller, file)
}

func loadVendorFromFile(fname string) (mtgban.Vendor, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return mtgban.ReadVendorFromJSON(file)
}

func dumpVendorToFile(vendor mtgban.Vendor, fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer file.Close()

	return mtgban.WriteVendorToJSON(vendor, file)
}

type scraperOption struct {
	// Scraper is busy, and there are active network requests
	Busy bool

	// The mutex to programmatically make a scraper busy
	Mutex sync.Mutex

	// Load data for this scraper in dev mode too
	DevEnabled bool

	// Disable any Vendor functionality associated with this scraper
	OnlySeller bool

	// Disable any Seller functionality associated with this scraper
	OnlyVendor bool

	// The initialization function used to allocate and initialize needed resources
	Init func(logger *log.Logger) (mtgban.Scraper, error)

	// For Market scrapers, list the sub-sellers that should be preserved
	Keepers []string

	// For Market scrapers, list the buylists that should be preserved
	KeepersBL string

	// The redis DBs where to stash data
	// For classic inventory/buylist the key is just "retail" and "buylist",
	// while for Market scrapers, the key is the name of the subseller
	RDBs map[string]*redis.Client

	// Save inventory data from this scraper to the associated redis DB
	StashInventory bool

	// Save buylist data from this scraper to the associated redis DB
	StashBuylist bool

	// Save market data from this scraper to the associated redis DB
	StashMarkets bool

	// Log where scrapers... log
	Logger *log.Logger
}

// Associate Scraper shorthands to ScraperOptions keys
var ScraperMap map[string]string

// Assiciate Scraper shorthands to Scraper Names
var ScraperNames map[string]string

// A default scale for converting non-NM prices to NM
var defaultGradeMap = map[string]float64{
	"NM": 1, "SP": 1.25, "MP": 1.67, "HP": 2.5, "PO": 4,
}

func updateStaticData() {
	if Infos == nil {
		Infos = map[string]mtgban.InventoryRecord{}
	}

	SealedEditionsSorted, SealedEditionsList = getSealedEditions()
	AllEditionsKeys, AllEditionsMap = getAllEditions()
	TreeEditionsKeys, TreeEditionsMap = getTreeEditions()
	ReprintsKeys, ReprintsMap = getReprintsGlobal()

	TotalSets = len(AllEditionsKeys)
	TotalUnique = len(mtgmatcher.GetUUIDs())
	var totalCards int
	for _, key := range AllEditionsKeys {
		totalCards += AllEditionsMap[key].Size
	}
	TotalCards = totalCards

	if !SkipInitialRefresh {
		go loadInfos()
	}
	go runSealedAnalysis()

	// Load prices for API users
	if !DevMode {
		go prepareCKAPI()
	}
}

func loadInfos() {
	log.Println("Loading infos")

	for _, seller := range []mtgban.Seller{
		mtgstocks.NewScraper(),
		mtgstocks.NewScraperIndex(),
		tcgplayer.NewScraperSYP(),
	} {
		loadInfoScraper(seller)
	}
	ServerNotify("refresh", "infos refreshed")
}

func loadInfoScraper(seller mtgban.Seller) {
	inv, err := seller.Inventory()
	if err != nil {
		log.Println(err)
		return
	}
	Infos[seller.Info().Shorthand] = inv
	log.Println("Infos loaded:", seller.Info().Name)
}

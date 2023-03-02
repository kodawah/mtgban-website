package main

import (
	"log"
	"os"

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
	CT_STANDARD = "Card Trader"
	CT_ZERO     = "Card Trader Zero"

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

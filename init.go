package main

import (
	"net/http"
	"runtime/debug"
	"sort"
	"time"

	"github.com/kodabb/go-mtgban/abugames"
	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/channelfireball"
	"github.com/kodabb/go-mtgban/coolstuffinc"
	"github.com/kodabb/go-mtgban/miniaturemarket"
	"github.com/kodabb/go-mtgban/ninetyfive"
	"github.com/kodabb/go-mtgban/strikezone"
	"github.com/kodabb/go-mtgban/tcgplayer"

	"log"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
)

func loadDB() error {
	respPrintings, err := http.Get("https://www.mtgjson.com/files/AllPrintings.json")
	if err != nil {
		return err
	}
	defer respPrintings.Body.Close()

	respCards, err := http.Get("https://www.mtgjson.com/files/AllCards.json")
	if err != nil {
		return err
	}
	defer respCards.Body.Close()

	return mtgdb.RegisterWithReaders(respPrintings.Body, respCards.Body)
}

func periodicFunction() {
	log.Println("Updating data")

	newbc := mtgban.NewClient()

	newck := cardkingdom.NewScraper()
	newck.Partner = CKPartner
	newck.LogCallback = log.Printf

	newsz := strikezone.NewScraper()
	newsz.LogCallback = log.Printf

	newabu := abugames.NewScraper()
	newabu.LogCallback = log.Printf

	newcfb := channelfireball.NewScraper()
	newcfb.LogCallback = log.Printf

	newmm := miniaturemarket.NewScraper()
	newmm.LogCallback = log.Printf

	new95 := ninetyfive.NewScraper()
	new95.LogCallback = log.Printf

	tcg := tcgplayer.NewScraperMarket(TCGConfig.PublicId, TCGConfig.PrivateId)
	tcg.Affiliate = TCGConfig.Affiliate
	tcg.LogCallback = log.Printf

	newcsi := coolstuffinc.NewScraper()
	newcsi.LogCallback = log.Printf

	newbc.Register(newck)
	newbc.Register(newsz)
	newbc.Register(new95)
	if !DevMode {
		newbc.Register(newabu)
		newbc.Register(newcfb)
		newbc.Register(newmm)
		newbc.Register(newcsi)
		newbc.RegisterVendor(tcg)

		sellers, err := mtgban.Seller2Sellers(tcg)
		if err != nil {
			log.Println(err)
		}
		for _, seller := range sellers {
			if seller.Info().Name == "TCG Low" || seller.Info().Name == "TCG Direct Low" {
				newbc.RegisterSeller(seller)
			}
		}
		debug.FreeOSMemory()
	}

	// Load inventory first and then buylists
	// Return as much memory as possible between runs to prevent running out
	// of memory quota on heroku
	newSellers := newbc.Sellers()
	sort.Slice(newSellers, func(i, j int) bool {
		return newSellers[i].Info().Name < newSellers[j].Info().Name
	})
	for _, seller := range newSellers {
		_, err := seller.Inventory()
		debug.FreeOSMemory()
		log.Println(seller.Info().Name)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("-- OK")
	}

	newVendors := newbc.Vendors()
	sort.Slice(newVendors, func(i, j int) bool {
		return newVendors[i].Info().Name < newVendors[j].Info().Name
	})
	for _, vendor := range newVendors {
		_, err := vendor.Buylist()
		debug.FreeOSMemory()
		log.Println(vendor.Info().Name)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("-- OK")
	}

	BanClient = newbc
	Sellers = newSellers
	Vendors = newVendors

	LastUpdate = time.Now()

	// Clean as much as possible to that we stay under quota
	debug.FreeOSMemory()

	log.Println("Scrapers loaded")
}

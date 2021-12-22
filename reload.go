package main

import (
	"log"

	"github.com/kodabb/go-mtgban/mtgban"
)

func reloadCK() {
	reloadSingle("cardkingdom")
}

func reloadCSI() {
	reloadSingle("coolstuffinc")
}

func reloadSCG() {
	reloadSingle("starcitygames")
}

func reloadSingle(name string) {
	defer recoverPanicScraper()

	log.Println("Reloading", name)

	ScraperOptions[name].Mutex.Lock()
	ScraperOptions[name].Busy = true
	defer func() {
		ScraperOptions[name].Busy = false
		ScraperOptions[name].Mutex.Unlock()
	}()

	scraper, err := ScraperOptions[name].Init(ScraperOptions[name].Logger)
	if err != nil {
		log.Println(err)
		return
	}

	// These functions will update the global scraper only if it was
	// previously added to the slice of Sellers or Vendors via the
	// client Register functions
	updateSellers(scraper)
	updateVendors(scraper)

	Notify("refresh", name+" refresh completed")
}

func reloadTCG() {
	reloadMarket("tcg_index")
	reloadMarket("tcg_market")

	Notify("refresh", "tcg fully refreshed")
}

func reloadMKM() {
	reloadMarket("cardmarket")

	Notify("refresh", "mkm fully refreshed")
}

func reloadMarket(name string) {
	defer recoverPanicScraper()

	log.Println("Reloading", name)

	ScraperOptions[name].Mutex.Lock()
	ScraperOptions[name].Busy = true
	defer func() {
		ScraperOptions[name].Busy = false
		ScraperOptions[name].Mutex.Unlock()
	}()

	scraper, err := ScraperOptions[name].Init(ScraperOptions[name].Logger)
	if err != nil {
		log.Println(err)
		return
	}

	multiSellers, err := mtgban.Seller2Sellers(scraper.(mtgban.Market))
	if err != nil {
		log.Println(err)
		return
	}

	for i := range multiSellers {
		updateSellers(multiSellers[i])
	}

	// This can be done because only the already-registered scrapers
	// will be updated, no effect otherwise
	updateVendors(scraper)

	Notify("refresh", name+" market refresh completed")
}

func updateSellers(scraper mtgban.Scraper) {
	for i := range Sellers {
		if Sellers[i] != nil && Sellers[i].Info().Shorthand == scraper.Info().Shorthand {
			inv, err := scraper.(mtgban.Seller).Inventory()
			if err != nil {
				log.Println(Sellers[i].Info().Name, "error", err)
				continue
			}
			if len(inv) == 0 {
				log.Println(Sellers[i].Info().Name, "empty inventory")
				continue
			}
			Sellers[i] = mtgban.NewSellerFromInventory(inv, scraper.Info())
			log.Println(Sellers[i].Info().Shorthand, "inventory updated")
		}
	}
}

func updateVendors(scraper mtgban.Scraper) {
	for i := range Vendors {
		if Vendors[i] != nil && Vendors[i].Info().Shorthand == scraper.Info().Shorthand {
			bl, err := scraper.(mtgban.Vendor).Buylist()
			if err != nil {
				log.Println(Vendors[i].Info().Name, "error", err)
				continue
			}
			if len(bl) == 0 {
				log.Println(Vendors[i].Info().Name, "empty buylist")
				continue
			}
			Vendors[i] = mtgban.NewVendorFromBuylist(bl, scraper.Info())
			log.Println(Vendors[i].Info().Shorthand, "buylist updated")
		}
	}
}

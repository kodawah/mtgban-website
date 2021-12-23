package main

import (
	"fmt"

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

	ServerNotify("refresh", "Reloading "+name)

	// Lock because we plan to load both sides of the scraper
	ScraperOptions[name].Mutex.Lock()
	ScraperOptions[name].Busy = true
	defer func() {
		ScraperOptions[name].Busy = false
		ScraperOptions[name].Mutex.Unlock()
	}()

	scraper, err := ScraperOptions[name].Init(ScraperOptions[name].Logger)
	if err != nil {
		msg := fmt.Sprintf("error initializing %s: %s", name, err.Error())
		ServerNotify("refresh", msg, true)
		return
	}

	// These functions will update the global scraper only if it was
	// previously added to the slice of Sellers or Vendors via the
	// client Register functions
	updateSellers(scraper)
	updateVendors(scraper)

	ServerNotify("refresh", name+" refresh completed")
}

func reloadTCG() {
	reloadMarket("tcg_index")
	reloadMarket("tcg_market")

	ServerNotify("refresh", "tcg fully refreshed")
}

func reloadMKM() {
	reloadMarket("cardmarket")

	ServerNotify("refresh", "mkm fully refreshed")
}

func reloadMarket(name string) {
	defer recoverPanicScraper()

	ServerNotify("refresh", "Reloading "+name)

	// Lock because we plan to load both sides of the scraper
	ScraperOptions[name].Mutex.Lock()
	ScraperOptions[name].Busy = true
	defer func() {
		ScraperOptions[name].Busy = false
		ScraperOptions[name].Mutex.Unlock()
	}()

	scraper, err := ScraperOptions[name].Init(ScraperOptions[name].Logger)
	if err != nil {
		msg := fmt.Sprintf("error initializing %s: %s", name, err.Error())
		ServerNotify("refresh", msg, true)
		return
	}

	multiSellers, err := mtgban.Seller2Sellers(scraper.(mtgban.Market))
	if err != nil {
		msg := fmt.Sprintf("error separating %s: %s", name, err.Error())
		ServerNotify("refresh", msg)
		return
	}

	keepers := ScraperOptions[name].Keepers
	for i := range multiSellers {
		// Skip subsellers not explicitly enabled
		if !SliceStringHas(keepers, multiSellers[i].Info().Shorthand) {
			continue
		}
		updateSellers(multiSellers[i])
	}

	// This can be done because only the already-registered scrapers
	// will be updated, no effect otherwise
	updateVendors(scraper)

	ServerNotify("refresh", name+" market refresh completed")
}

func updateSellers(scraper mtgban.Scraper) {
	for i := range Sellers {
		if Sellers[i] != nil && Sellers[i].Info().Shorthand == scraper.Info().Shorthand {
			updateSellerAtPosition(scraper.(mtgban.Seller), i, false)
			ServerNotify("refresh", scraper.Info().Shorthand+" inventory updated")
		}
	}
}

func updateSellerAtPosition(seller mtgban.Seller, i int, andLock bool) {
	opts := ScraperOptions[ScraperMap[seller.Info().Shorthand]]

	if andLock {
		opts.Mutex.Lock()
		opts.Busy = true
		defer func() {
			opts.Busy = false
			opts.Mutex.Unlock()
		}()
	}

	// Load inventory
	inv, err := seller.Inventory()
	if err != nil {
		msg := fmt.Sprintf("seller %s %s - error %s", seller.Info().Name, seller.Info().Shorthand, err.Error())
		ServerNotify("refresh", msg, true)
		return
	}
	if len(inv) == 0 {
		msg := fmt.Sprintf("seller %s %s - empty inventory", seller.Info().Name, seller.Info().Shorthand)
		ServerNotify("refresh", msg, true)
		return
	}

	// Save seller in global array, making sure it's _only_ a Seller
	// and not anything esle, so that filtering works like expected
	Sellers[i] = mtgban.NewSellerFromInventory(inv, seller.Info())
}

func updateVendors(scraper mtgban.Scraper) {
	for i := range Vendors {
		if Vendors[i] != nil && Vendors[i].Info().Shorthand == scraper.Info().Shorthand {
			updateVendorAtPosition(scraper.(mtgban.Vendor), i, false)
			ServerNotify("refresh", scraper.Info().Shorthand+" buylist updated")
		}
	}
}

func updateVendorAtPosition(vendor mtgban.Vendor, i int, andLock bool) {
	opts := ScraperOptions[ScraperMap[vendor.Info().Shorthand]]

	if andLock {
		opts.Mutex.Lock()
		opts.Busy = true
		defer func() {
			opts.Busy = false
			opts.Mutex.Unlock()
		}()
	}

	// Load buylist
	bl, err := vendor.Buylist()
	if err != nil {
		msg := fmt.Sprintf("vendor %s %s - error %s", vendor.Info().Name, vendor.Info().Shorthand, err.Error())
		ServerNotify("refresh", msg, true)
		return
	}
	if len(bl) == 0 {
		msg := fmt.Sprintf("vendor %s %s - empty buylist", vendor.Info().Name, vendor.Info().Shorthand)
		ServerNotify("refresh", msg, true)
		return
	}

	// Save vendor in global array, making sure it's _only_ a Vendor
	// and not anything esle, so that filtering works like expected
	Vendors[i] = mtgban.NewVendorFromBuylist(bl, vendor.Info())
}

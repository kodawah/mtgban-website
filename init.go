package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"time"

	"github.com/kodabb/go-mtgban/abugames"
	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/channelfireball"
	"github.com/kodabb/go-mtgban/coolstuffinc"
	"github.com/kodabb/go-mtgban/facetoface"
	"github.com/kodabb/go-mtgban/miniaturemarket"
	"github.com/kodabb/go-mtgban/ninetyfive"
	"github.com/kodabb/go-mtgban/starcitygames"
	"github.com/kodabb/go-mtgban/strikezone"
	"github.com/kodabb/go-mtgban/tcgplayer"
	"github.com/kodabb/go-mtgban/trollandtoad"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
)

const (
	TCG_LOW        = "TCG Low"
	TCG_DIRECT_LOW = "TCG Direct Low"
	TCG_BUYLIST    = "TCG Player"
)

func loadDB() error {
	return mtgdb.RegisterWithPaths("allprintings.json", "allcards.json")
}

func loadInventoryFromFile(info mtgban.ScraperInfo, fname string) (mtgban.Seller, error) {
	// Get file path from symlink
	link, err := os.Readlink(fname)
	if err != nil {
		return nil, err
	}

	log.Println("File dump found:", link)
	// Open file (not the symlink)
	file, err := os.Open(link)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Load inventory
	inv, err := mtgban.LoadInventoryFromCSV(file)
	if err != nil {
		return nil, err
	}

	// Create seller using the properties of the scraper
	info.InventoryTimestamp = fileDate(link)

	return mtgban.NewSellerFromInventory(inv, info), nil
}

func dumpInventoryToFile(seller mtgban.Seller, currentDir, fname string) error {
	// Create dump file
	outName := currentDir + "/" + seller.Info().Name + ".csv"
	file, err := os.Create(outName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write everything to dump file
	err = mtgban.WriteInventoryToCSV(seller, file)
	if err != nil {
		return err
	}

	// Link dumpfile to the latest available source
	os.Remove(fname)
	return os.Symlink(outName, fname)
}

func loadBuylistFromFile(info mtgban.ScraperInfo, fname string) (mtgban.Vendor, error) {
	// Get file path from symlink
	link, err := os.Readlink(fname)
	if err != nil {
		return nil, err
	}

	log.Println("File dump found:", link)
	// Open file (not the symlink)
	file, err := os.Open(link)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Load inventory
	bl, err := mtgban.LoadBuylistFromCSV(file)
	if err != nil {
		return nil, err
	}

	// Create seller using the properties of the scraper
	info.BuylistTimestamp = fileDate(link)

	return mtgban.NewVendorFromBuylist(bl, info), nil
}

func dumpBuylistToFile(vendor mtgban.Vendor, currentDir, fname string) error {
	// Create dump file
	outName := currentDir + "/" + vendor.Info().Name + ".csv"
	file, err := os.Create(outName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write everything to dump file
	err = mtgban.WriteBuylistToCSV(vendor, file)
	if err != nil {
		return err
	}

	// Link dumpfile to the latest available source
	os.Remove(fname)
	return os.Symlink(outName, fname)
}

func specialTCGhandle(init bool, currentDir string, newbc *mtgban.BanClient, tcg mtgban.Market) error {
	dirName := path.Clean(currentDir+"/..") + "/"

	// Check if both sub seller files are present
	lowname := dirName + "TCG Low-latest.csv"
	lowdirectname := dirName + "TCG Direct Low-latest.csv"
	if init && fileExists(lowname) && fileExists(lowdirectname) {
		log.Println("Found TCG subseller files")

		for _, name := range []string{TCG_LOW, TCG_DIRECT_LOW} {
			info := tcg.Info()
			info.Name = name
			info.Shorthand = name
			// Empty inventory, since the real loading will happen later
			seller := mtgban.NewSellerFromInventory(nil, info)
			newbc.Register(seller)
			log.Println("Spoofed", name)
		}
		return nil
	}

	sellers := []mtgban.Seller{}
	var err error

	// Check if the main file is present and load it
	fname := dirName + tcg.Info().Name + "-latest.csv"
	if init && fileExists(fname) {
		log.Println("Found TCG Market file")

		seller, err := loadInventoryFromFile(tcg.Info(), fname)
		if err != nil {
			return err
		}
		sellers, err = mtgban.Seller2Sellers(seller.(mtgban.Market))
		if err != nil {
			return err
		}

		log.Println("TCG Market preloaded from file")
	} else {
		// Split subsellers (either from file or from web)
		sellers, err = mtgban.Seller2Sellers(tcg)
		if err != nil {
			return err
		}

		// Dump main file
		err = dumpInventoryToFile(tcg, currentDir, fname)
		if err != nil {
			return err
		}
		log.Println("Dumped", fname)
	}

	// Save and register sellers that matter
	for _, seller := range sellers {
		if seller.Info().Name == TCG_LOW || seller.Info().Name == TCG_DIRECT_LOW {
			fname := dirName + seller.Info().Name + "-latest.csv"
			err = dumpInventoryToFile(seller, currentDir, fname)
			if err != nil {
				return err
			}
			newbc.RegisterSeller(seller)
			log.Println("Dumped", fname)
		}
	}

	return nil
}

func loadCK() {
	log.Println("Reloading CK")

	newck := cardkingdom.NewScraper()
	newck.Partner = Config.Affiliate["CK"]
	newck.LogCallback = log.Printf

	for i := range Sellers {
		if Sellers[i] != nil && Sellers[i].Info().Shorthand == "CK" {
			_, err := newck.Inventory()
			if err != nil {
				log.Println(err)
				continue
			}
			log.Println("CK Inventory updated")
			Sellers[i] = newck
		}
	}

	for i := range Vendors {
		if Vendors[i] != nil && Vendors[i].Info().Shorthand == "CK" {
			_, err := newck.Buylist()
			if err != nil {
				log.Println(err)
				continue
			}
			log.Println("CK Buylist updated")
			Vendors[i] = newck
		}
	}
}

type scraperOption struct {
	DevEnabled bool
	OnlySeller bool
	OnlyVendor bool
	Init       func() (mtgban.Scraper, error)
}

var options = map[string]*scraperOption{
	"abugames": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := abugames.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"cardkingdom": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := cardkingdom.NewScraper()
			scraper.LogCallback = log.Printf
			scraper.Partner = Config.Affiliate["CK"]
			return scraper, nil
		},
	},
	"channelfireball": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := channelfireball.NewScraper()
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 6
			return scraper, nil
		},
	},
	"coolstuffinc": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := coolstuffinc.NewScraper()
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 6
			return scraper, nil
		},
	},
	"facetoface": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper, err := facetoface.NewScraper()
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"miniaturemarket": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := miniaturemarket.NewScraper()
			scraper.LogCallback = log.Printf
			scraper.Affiliate = "MTGBAN"
			return scraper, nil
		},
	},
	"ninetyfive": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := ninetyfive.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"starcitygames": &scraperOption{
		OnlyVendor: true,
		Init: func() (mtgban.Scraper, error) {
			resp, err := http.Get(Config.Api["scg_categories"])
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			scraper, err := starcitygames.NewScraper(resp.Body)
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"strikezone": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := strikezone.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"trollandtoad": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := trollandtoad.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"tcg_market": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperMarket(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
}

func loadScrapers(doSellers, doVendors bool) {
	init := !DatabaseLoaded
	if init {
		log.Println("Loading data")
	} else {
		log.Println("Updating data")
	}

	dirName := "cache_inv/"
	currentDir := fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
	mkDirIfNotExisting(currentDir)

	newbc := mtgban.NewClient()

	for key, opt := range options {
		if DevMode && !opt.DevEnabled {
			continue
		}
		scraper, err := opt.Init()
		if err != nil {
			log.Println("error initializing", key, err)
			continue
		}

		if key == "tcg_market" {
			err := specialTCGhandle(init, currentDir, newbc, scraper.(mtgban.Market))
			if err != nil {
				log.Println(err)
			}
			newbc.RegisterVendor(scraper)
		} else if opt.OnlySeller {
			newbc.RegisterSeller(scraper)
		} else if opt.OnlyVendor {
			newbc.RegisterVendor(scraper)
		} else {
			newbc.Register(scraper)
		}
	}

	// Sort the sellers/vendors arrays by name
	//
	// Note that pointers are shared between these two arrays,
	// things like Price Ratio (bl data depending on inv data)
	// still work just fine, even if we don't use them in the
	// global arrays in the end.
	newSellers := newbc.Sellers()
	sort.Slice(newSellers, func(i, j int) bool {
		return newSellers[i].Info().Name < newSellers[j].Info().Name
	})
	newVendors := newbc.Vendors()
	sort.Slice(newVendors, func(i, j int) bool {
		return newVendors[i].Info().Name < newVendors[j].Info().Name
	})

	// Allocate enough space for the global pointers
	if Sellers == nil {
		Sellers = make([]mtgban.Seller, len(newSellers))
	}
	if Vendors == nil {
		Vendors = make([]mtgban.Vendor, len(newVendors))
	}

	if doSellers {
		loadSellers(newSellers)
	}
	if doVendors {
		loadVendors(newVendors)
	}

	LastUpdate = time.Now()

	log.Println("Scrapers loaded")
}

func loadSellers(newSellers []mtgban.Seller) {
	init := !DatabaseLoaded
	dirName := "cache_inv/"
	currentDir := fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
	mkDirIfNotExisting(currentDir)

	// Load Sellers
	for i := range newSellers {
		log.Println(newSellers[i].Info().Name, "Inventory")

		fname := dirName + newSellers[i].Info().Name + "-latest.csv"
		if init && fileExists(fname) {
			seller, err := loadInventoryFromFile(newSellers[i].Info(), fname)
			if err != nil {
				log.Println(err)
				continue
			}
			Sellers[i] = seller

			log.Println("Loaded from file")
		} else {
			log.Println("Loading from scraper")

			// Load inventory
			_, err := newSellers[i].Inventory()
			if err != nil {
				log.Println(err)
				continue
			}

			// Save seller in global array
			Sellers[i] = newSellers[i]

			err = dumpInventoryToFile(Sellers[i], currentDir, fname)
			if err != nil {
				log.Println(err)
				continue
			}

			log.Println("Saved to file")
		}
		log.Println("-- OK")
	}
}

func loadVendors(newVendors []mtgban.Vendor) {
	init := !DatabaseLoaded
	dirName := "cache_bl/"
	currentDir := fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
	mkDirIfNotExisting(currentDir)

	// Load Vendors
	for i := range newVendors {
		log.Println(newVendors[i].Info().Name, "Buylist")

		fname := dirName + newVendors[i].Info().Name + "-latest.csv"
		if init && fileExists(fname) {
			vendor, err := loadBuylistFromFile(newVendors[i].Info(), fname)
			if err != nil {
				log.Println(err)
				continue
			}
			Vendors[i] = vendor

			log.Println("Loaded from file")
		} else {
			log.Println("Loading from scraper")

			// Load inventory
			_, err := newVendors[i].Buylist()
			if err != nil {
				log.Println(err)
				continue
			}

			// Save vendor in global array
			Vendors[i] = newVendors[i]

			err = dumpBuylistToFile(Vendors[i], currentDir, fname)
			if err != nil {
				log.Println(err)
				continue
			}

			log.Println("Saved to file")
		}
		log.Println("-- OK")
	}
}

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/kodabb/go-mtgban/abugames"
	"github.com/kodabb/go-mtgban/amazon"
	"github.com/kodabb/go-mtgban/blueprint"
	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/cardmarket"
	"github.com/kodabb/go-mtgban/cardsphere"
	"github.com/kodabb/go-mtgban/cardtrader"
	"github.com/kodabb/go-mtgban/coolstuffinc"
	"github.com/kodabb/go-mtgban/hareruya"
	"github.com/kodabb/go-mtgban/magiccorner"
	"github.com/kodabb/go-mtgban/mtgseattle"
	"github.com/kodabb/go-mtgban/mtgstocks"
	"github.com/kodabb/go-mtgban/mythicmtg"
	"github.com/kodabb/go-mtgban/ninetyfive"
	"github.com/kodabb/go-mtgban/purplemana"
	"github.com/kodabb/go-mtgban/starcitygames"
	"github.com/kodabb/go-mtgban/strikezone"
	"github.com/kodabb/go-mtgban/tcgplayer"
	"github.com/kodabb/go-mtgban/trollandtoad"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	// from TCGIndex
	TCG_LOW    = "TCG Low"
	TCG_MARKET = "TCG Market"

	// from TCGMrkt
	TCG_MAIN    = "TCG Player"
	TCG_DIRECT  = "TCG Direct"
	TCG_BUYLIST = "TCG Player Market"

	// from MKMIndex
	MKM_LOW   = "MKM Low"
	MKM_TREND = "MKM Trend"

	SkipRefreshCooldown = 2 * time.Hour
)

func loadDatastore() error {
	allPrintingsReader, err := os.Open("allprintings5.json")
	if err != nil {
		return err
	}
	defer allPrintingsReader.Close()

	return mtgmatcher.LoadDatastore(allPrintingsReader)
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
	inv, err := mtgban.LoadInventoryFromCSV(file, false)
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
	bl, err := mtgban.LoadBuylistFromCSV(file, false)
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

func untangleMarket(init bool, currentDir string, newbc *mtgban.BanClient, scraper mtgban.Market, key string) error {
	names := ScraperOptions[key].Keepers
	log.Println("Untangling", scraper.Info().Shorthand, "to", names)

	dirName := path.Clean(currentDir+"/..") + "/"

	for _, name := range names {
		ScraperMap[name] = key
	}
	// Check if both sub seller files are present
	if init {
		ok := true
		for _, name := range names {
			if !fileExists(dirName + name + "-latest.csv") {
				ok = false
				break
			}
		}

		if ok {
			log.Println("Found Market subvendor files")

			for _, name := range names {
				info := scraper.Info()
				info.Name = name
				info.Shorthand = name
				// Empty inventory, since the real loading will happen later
				seller := mtgban.NewSellerFromInventory(nil, info)
				newbc.Register(seller)
				log.Println("Spoofed", name)
			}
			return nil
		}
	} else {
		// Check if recent data already exists
		for _, seller := range Sellers {
			for _, name := range names {
				if seller != nil && seller.Info().Shorthand == name && time.Now().Sub(seller.Info().InventoryTimestamp) < SkipRefreshCooldown {
					log.Println("Skipping", scraper.Info().Name, "because too recent")
					return nil
				}
			}
		}
	}

	var sellers []mtgban.Seller
	var err error

	// Check if the main file is present and load it
	fname := dirName + scraper.Info().Name + "-latest.csv"
	if init && fileExists(fname) {
		log.Println("Found", scraper.Info().Name, "main file")

		seller, err := loadInventoryFromFile(scraper.Info(), fname)
		if err != nil {
			return err
		}
		sellers, err = mtgban.Seller2Sellers(seller.(mtgban.Market))
		if err != nil {
			return err
		}

		log.Println(scraper.Info().Name, "preloaded from file")
	} else {
		// Preload the market
		ScraperOptions[key].Mutex.Lock()
		ScraperOptions[key].Busy = true
		inv, err := scraper.Inventory()
		ScraperOptions[key].Busy = false
		ScraperOptions[key].Mutex.Unlock()
		if err != nil || len(inv) == 0 {
			// If a fallback file exists, try loading that
			if fileExists(fname) {
				log.Println("Market preload failed with", err)
				seller, err := loadInventoryFromFile(scraper.Info(), fname)
				if err != nil {
					return err
				}
				var ok bool
				scraper, ok = seller.(mtgban.Market)
				if !ok {
					return fmt.Errorf("%s is not a Market", scraper.Info().Name)
				}
			} else {
				if len(inv) == 0 {
					err = errors.New("empty inventory")
				}
				return err
			}
		}

		// Split subsellers
		sellers, err = mtgban.Seller2Sellers(scraper)
		if err != nil {
			return err
		}

		// Dump main file
		err = dumpInventoryToFile(scraper, currentDir, fname)
		if err != nil {
			return err
		}
		log.Println("Dumped main file for", scraper.Info().Name, "to", fname)
	}

	// Save and register sellers that were requested earlier
	notdone := make([]string, len(names))
	copy(notdone, names)
	for _, seller := range sellers {
		for i := range notdone {
			if seller.Info().Name == notdone[i] {
				fname := dirName + notdone[i] + "-latest.csv"
				err = dumpInventoryToFile(seller, currentDir, fname)
				if err != nil {
					log.Println(scraper.Info().Name, "errored with", err)
				} else {
					newbc.RegisterSeller(seller)
					log.Println("Dumped", fname)
				}
				// Mark as done
				notdone[i] = ""
			}
		}
	}

	return nil
}

type scraperOption struct {
	Busy       bool
	Mutex      sync.Mutex
	DevEnabled bool
	OnlySeller bool
	OnlyVendor bool
	Init       func() (mtgban.Scraper, error)
	Keepers    []string
	KeepersBL  string
}

var ScraperOptions = map[string]*scraperOption{
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
	"coolstuffinc": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := coolstuffinc.NewScraper()
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 4
			return scraper, nil
		},
	},
	"ninetyfive": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := ninetyfive.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"starcitygames": &scraperOption{
		OnlyVendor: true,
		Init: func() (mtgban.Scraper, error) {
			scraper, err := starcitygames.NewScraper(Config.Api["scg_username"], Config.Api["scg_password"])
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
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperMarket(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 5
			return scraper, nil
		},
		Keepers:   []string{TCG_MAIN, TCG_DIRECT},
		KeepersBL: TCG_BUYLIST,
	},
	"tcg_index": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperIndex(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 4
			return scraper, nil
		},
		Keepers: []string{TCG_LOW, TCG_MARKET},
	},
	"magiccorner": &scraperOption{
		OnlySeller: true,
		Init: func() (mtgban.Scraper, error) {
			scraper, err := magiccorner.NewScraper()
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"blueprint": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := blueprint.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"mythicmtg": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := mythicmtg.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"cardmarket": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper, err := cardmarket.NewScraperIndex(Config.Api["mkm_app_token"], Config.Api["mkm_app_secret"])
			if err != nil {
				return nil, err
			}
			scraper.Affiliate = Config.Affiliate["MKM"]
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
		Keepers: []string{MKM_LOW, MKM_TREND},
	},
	"cardsphere": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper, err := cardsphere.NewScraperFull(Config.Api["csphere_email"], Config.Api["csphere_password"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			scraper.MaxConcurrency = 3
			return scraper, nil
		},
	},
	"amazon": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := amazon.NewScraper(Config.Api["amz_token"])
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"cardtrader": &scraperOption{
		DevEnabled: true,
		Init: func() (mtgban.Scraper, error) {
			scraper := cardtrader.NewScraperMarket(Config.Api["cardtrader"])
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"mtgseattle": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper := mtgseattle.NewScraper()
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"purplemana": &scraperOption{
		Init: func() (mtgban.Scraper, error) {
			scraper, err := purplemana.NewScraper(Config.Api["service_account_file"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
	"hareruya": &scraperOption{
		OnlyVendor: true,
		Init: func() (mtgban.Scraper, error) {
			scraper, err := hareruya.NewScraper()
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = log.Printf
			return scraper, nil
		},
	},
}

// Associate Scraper shorthands to ScraperOptions keys
var ScraperMap map[string]string

func loadScrapers(doSellers, doVendors bool) {
	init := !DatabaseLoaded
	if init {
		log.Println("Loading data")
		Notify("init", "loading started")
	} else {
		log.Println("Updating data")
		Notify("refresh", "full refresh started")
	}

	dirName := "cache_inv/"
	currentDir := fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
	mkDirIfNotExisting(currentDir)

	newbc := mtgban.NewClient()

	// Keep track of the names used in the options table, so that we can
	// reference the mutex more freely
	if ScraperMap == nil {
		ScraperMap = map[string]string{}
	}

	for key, opt := range ScraperOptions {
		if DevMode && !opt.DevEnabled {
			continue
		}
		scraper, err := opt.Init()
		if err != nil {
			log.Println("error initializing", key, err)
			continue
		}

		if len(opt.Keepers) != 0 {
			if !opt.OnlyVendor {
				err := untangleMarket(init, currentDir, newbc, scraper.(mtgban.Market), key)
				if err != nil {
					log.Println("failed to load", key)
					log.Println(err)
				}
			}
			if !opt.OnlySeller {
				newbc.RegisterVendor(scraper)
			}
		} else if opt.OnlySeller {
			newbc.RegisterSeller(scraper)
		} else if opt.OnlyVendor {
			newbc.RegisterVendor(scraper)
		} else {
			newbc.Register(scraper)
		}
		ScraperMap[scraper.Info().Shorthand] = key
	}

	log.Println(ScraperMap)

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
	if Infos == nil {
		Infos = map[string]mtgban.InventoryRecord{}
	}

	if doSellers {
		log.Println("Sellers table")
		for i := range newSellers {
			if newSellers[i] == nil {
				log.Println(i, "<nil>")
				continue
			}
			log.Println(i, newSellers[i].Info().Name)
		}
		loadSellers(newSellers)
	}
	if doVendors {
		log.Println("Vendors table")
		for i := range newVendors {
			if newVendors[i] == nil {
				log.Println(i, "<nil>")
				continue
			}
			log.Println(i, newVendors[i].Info().Name)
		}
		loadVendors(newVendors)
	}

	go loadInfos()

	// Load prices for API users
	if !DevMode {
		go prepareCKAPI()
	}

	LastUpdate = time.Now()

	log.Println("Scrapers loaded")
	if init {
		Notify("init", "loading completed")
	} else {
		Notify("refresh", "full refresh completed")
	}
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
			if Sellers[i] != nil && time.Now().Sub(Sellers[i].Info().InventoryTimestamp) < SkipRefreshCooldown {
				log.Println("Skipping because too recent")
				continue
			}
			log.Println("Loading from scraper")

			// Load inventory
			ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]].Mutex.Lock()
			ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]].Busy = true
			inv, err := newSellers[i].Inventory()
			ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]].Busy = false
			ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]].Mutex.Unlock()
			if err != nil {
				log.Println(newSellers[i].Info().Name, "error", err)
				continue
			}
			if len(inv) == 0 {
				log.Println(newSellers[i].Info().Name, "empty inventory")
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
			if Vendors[i] != nil && time.Now().Sub(Vendors[i].Info().BuylistTimestamp) < SkipRefreshCooldown {
				log.Println("Skipping because too recent")
				continue
			}
			log.Println("Loading from scraper")

			// Load buylist
			ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]].Mutex.Lock()
			ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]].Busy = true
			bl, err := newVendors[i].Buylist()
			ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]].Busy = false
			ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]].Mutex.Unlock()
			if err != nil {
				log.Println(newVendors[i].Info().Name, "error", err)
				continue
			}
			if len(bl) == 0 {
				log.Println(newVendors[i].Info().Name, "empty buylist")
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

func loadInfos() {
	seller := mtgstocks.NewScraper()
	inv, err := seller.Inventory()
	if err != nil {
		log.Println(err)
		return
	}
	Infos[seller.Info().Shorthand] = inv
	log.Println("stocks refreshed")
	Notify("refresh", "stocks refreshed")
}

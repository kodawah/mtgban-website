package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/leemcloughlin/logfile"

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
	var needsLoading bool

	// Try reading from the two sub seller files (the main file is not dumped)
	if init {
		// Both files need to be present
		ok := true
		for _, name := range names {
			subfname := dirName + name + "-latest.csv"
			if !fileExists(subfname) {
				ok = false
				break
			}
		}

		if ok {
			for _, name := range names {
				subfname := dirName + name + "-latest.csv"

				// Override data from the main market scraper
				info := scraper.Info()
				info.Name = name
				info.Shorthand = name

				seller, err := loadInventoryFromFile(info, subfname)
				if err != nil {
					return err
				}

				// Register so that it will be added to the main Sellers array
				newbc.Register(seller)

				log.Println("Loaded from file")
			}

			log.Println("-- OK")
			return nil
		}

		// If one of the seller files is missing we need to load from scraper
		needsLoading = true
	} else {
		// Check if recent data already exists
		for _, seller := range Sellers {
			// In case there are any nil members just load everything
			if seller == nil {
				needsLoading = true
				continue
			}

			// Load if all the sellers inventory timestamps are past the cooldown
			if stringSliceContains(names, seller.Info().Name) && time.Now().Sub(seller.Info().InventoryTimestamp) < SkipRefreshCooldown {
				log.Println("Trying to skip", seller.Info().Name, "because too recent")
			} else {
				needsLoading = true
			}
		}
	}

	if needsLoading {
		log.Println("Loading from Market scraper")

		// Preload the market
		ScraperOptions[key].Mutex.Lock()
		ScraperOptions[key].Busy = true
		inv, err := scraper.Inventory()
		ScraperOptions[key].Busy = false
		ScraperOptions[key].Mutex.Unlock()
		if err != nil {
			return err
		}
		if len(inv) == 0 {
			return errors.New("empty inventory")
		}

		// Split subsellers
		sellers, err := mtgban.Seller2Sellers(scraper)
		if err != nil {
			return err
		}

		// Dump files for the requested sellers
		for _, seller := range sellers {
			if stringSliceContains(names, seller.Info().Shorthand) {
				// In case something went wrong earlier
				if init {
					newbc.Register(seller)
				}

				fname := dirName + seller.Info().Name + "-latest.csv"

				err = dumpInventoryToFile(seller, currentDir, fname)
				if err != nil {
					log.Println(err)
					continue
				}

				log.Println(seller.Info().Name, "saved to file")
			}
		}

		// Stash to redis if requested
		if ScraperOptions[key].StashMarkets {
			for _, seller := range sellers {
				db, found := ScraperOptions[key].MarketRDBs[seller.Info().Shorthand]
				if !found {
					continue
				}

				start := time.Now()
				log.Printf("Stashing %s inventory data to DB", seller.Info().Shorthand)
				inv, _ := seller.Inventory()
				for uuid, entries := range inv {
					err = db.HSet(context.Background(), uuid, seller.Info().InventoryTimestamp, entries[0].Price).Err()
					if err != nil {
						log.Printf("redis error for %s: %s", uuid, err)
					}
				}
				log.Println("Took", time.Now().Sub(start))
			}
		}
	}

	return nil
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

	// The redis DB where to stash data
	RDB *redis.Client

	// Save buylist data from this scraper to the associated redis DB
	StashBuylist bool

	// Save market data from this scraper to the associated redis DB
	StashMarkets bool

	// For Market scrapers, the array of redis DBs where each
	MarketRDBs map[string]*redis.Client

	// Log where scrapers... log
	Logger *log.Logger
}

var ScraperOptions = map[string]*scraperOption{
	"abugames": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := abugames.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"cardkingdom": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := cardkingdom.NewScraper()
			scraper.LogCallback = logger.Printf
			scraper.Partner = Config.Affiliate["CK"]
			return scraper, nil
		},
		RDB: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
			DB:   1,
		}),
		StashBuylist: true,
	},
	"coolstuffinc": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := coolstuffinc.NewScraperOfficial(Config.Api["csi_token"])
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"ninetyfive": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, _ := ninetyfive.NewScraper(false)
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"starcitygames": &scraperOption{
		OnlyVendor: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := starcitygames.NewScraper(Config.Api["scg_username"], Config.Api["scg_password"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"strikezone": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := strikezone.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"trollandtoad": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := trollandtoad.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"tcg_market": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperMarket(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = logger.Printf
			scraper.MaxConcurrency = 5
			return scraper, nil
		},
		Keepers:   []string{TCG_MAIN, TCG_DIRECT},
		KeepersBL: TCG_BUYLIST,
	},
	"tcg_index": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperIndex(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = logger.Printf
			scraper.MaxConcurrency = 4
			return scraper, nil
		},
		Keepers:      []string{TCG_LOW, TCG_MARKET},
		StashMarkets: true,
		MarketRDBs: map[string]*redis.Client{
			TCG_LOW: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   2,
			}),
			TCG_MARKET: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   3,
			}),
		},
	},
	"magiccorner": &scraperOption{
		OnlySeller: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := magiccorner.NewScraper()
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"blueprint": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := blueprint.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"mythicmtg": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := mythicmtg.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"cardmarket": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := cardmarket.NewScraperIndex(Config.Api["mkm_app_token"], Config.Api["mkm_app_secret"])
			if err != nil {
				return nil, err
			}
			scraper.Affiliate = Config.Affiliate["MKM"]
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
		Keepers: []string{MKM_LOW, MKM_TREND},
	},
	"cardsphere": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := cardsphere.NewScraperFull(Config.Api["csphere_email"], Config.Api["csphere_password"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
			scraper.MaxConcurrency = 3
			return scraper, nil
		},
	},
	"amazon": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := amazon.NewScraper(Config.Api["amz_token"])
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"cardtrader": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := cardtrader.NewScraperMarket(Config.Api["cardtrader"])
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"mtgseattle": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := mtgseattle.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"purplemana": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := purplemana.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"hareruya": &scraperOption{
		OnlyVendor: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := hareruya.NewScraper()
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
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

		// Create the destination logfile if not existing
		if opt.Logger == nil {
			logFile, err := logfile.New(&logfile.LogFile{
				FileName:    path.Join(LogDir, key+".log"),
				MaxSize:     500 * 1024,       // 500K duh!
				Flags:       logfile.FileOnly, // Default append
				OldVersions: 2,
			})
			if err != nil {
				log.Println("Failed to create logFile for %s: %s", key, err)
				opt.Logger = log.New(os.Stderr, "", log.LstdFlags)
				continue
			}
			opt.Logger = log.New(logFile, "", log.LstdFlags)
		}

		scraper, err := opt.Init(opt.Logger)
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

			opts := ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]]

			// Load inventory
			opts.Mutex.Lock()
			opts.Busy = true
			inv, err := newSellers[i].Inventory()
			opts.Busy = false
			opts.Mutex.Unlock()
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

			opts := ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]]

			// Load buylist
			opts.Mutex.Lock()
			opts.Busy = true
			bl, err := newVendors[i].Buylist()
			opts.Busy = false
			opts.Mutex.Unlock()
			if err != nil {
				log.Println(newVendors[i].Info().Name, "error", err)
				continue
			}
			if len(bl) == 0 {
				log.Println(newVendors[i].Info().Name, "empty buylist")
				continue
			}

			// Stash data to DB if requested
			if opts.StashBuylist {
				start := time.Now()
				log.Printf("Stashing %s buylist data to DB", newVendors[i].Info().Name)
				for uuid, entries := range bl {
					err = opts.RDB.HSet(context.Background(), uuid, newVendors[i].Info().BuylistTimestamp, entries[0].BuyPrice).Err()
					if err != nil {
						log.Printf("redis error for %s: %s", uuid, err)
					}
				}
				log.Println("Took", time.Now().Sub(start))
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

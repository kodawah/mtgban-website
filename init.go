package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
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
	"github.com/kodabb/go-mtgban/starcitygames"
	"github.com/kodabb/go-mtgban/strikezone"
	"github.com/kodabb/go-mtgban/tcgplayer"
	"github.com/kodabb/go-mtgban/toamagic"
	"github.com/kodabb/go-mtgban/trollandtoad"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgmatcher"
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

	SkipRefreshCooldown    = 2 * time.Hour
	DefaultUploaderTimeout = 60 * time.Second

	AllPrintingsFileName = "allprintings5.json"

	InventoryDir = "cache_inv"
	BuylistDir   = "cache_bl"
)

func loadDatastore() error {
	allPrintingsReader, err := os.Open(AllPrintingsFileName)
	if err != nil {
		return err
	}
	defer allPrintingsReader.Close()

	return mtgmatcher.LoadDatastore(allPrintingsReader)
}

func loadInventoryFromFile(fname string) (mtgban.Seller, error) {
	// Get file path from symlink
	link, err := os.Readlink(fname)
	if err != nil {
		return nil, err
	}

	log.Println("File dump found:", link)
	return loadSellerFromFile(link)
}

func loadSellerFromFile(fname string) (mtgban.Seller, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return mtgban.ReadSellerFromJSON(file)
}

func uploadSeller(seller mtgban.Seller, currentDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	outName := path.Join(currentDir, seller.Info().Shorthand+".json")
	wc := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(outName).NewWriter(ctx)
	wc.ContentType = "application/json"
	defer wc.Close()

	return mtgban.WriteSellerToJSON(seller, wc)
}

func uploadVendor(vendor mtgban.Vendor, currentDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	outName := path.Join(currentDir, vendor.Info().Shorthand+".json")
	wc := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(outName).NewWriter(ctx)
	wc.ContentType = "application/json"
	defer wc.Close()

	return mtgban.WriteVendorToJSON(vendor, wc)
}

func dumpInventoryToFile(seller mtgban.Seller, currentDir, fname string) error {
	outName := path.Join(currentDir, seller.Info().Shorthand+".json")

	// Create dump file
	err := dumpSellerToFile(seller, outName)
	if err != nil {
		return err
	}

	// Link dumpfile to the latest available source
	os.Remove(fname)
	return os.Symlink(outName, fname)
}

func dumpSellerToFile(seller mtgban.Seller, fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer file.Close()

	return mtgban.WriteSellerToJSON(seller, file)
}

func loadBuylistFromFile(fname string) (mtgban.Vendor, error) {
	// Get file path from symlink
	link, err := os.Readlink(fname)
	if err != nil {
		return nil, err
	}

	log.Println("File dump found:", link)
	return loadVendorFromFile(link)
}

func loadVendorFromFile(fname string) (mtgban.Vendor, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return mtgban.ReadVendorFromJSON(file)
}

func dumpBuylistToFile(vendor mtgban.Vendor, currentDir, fname string) error {
	outName := path.Join(currentDir, vendor.Info().Shorthand+".json")

	// Create dump file
	err := dumpVendorToFile(vendor, outName)
	if err != nil {
		return err
	}

	// Link dumpfile to the latest available source
	os.Remove(fname)
	return os.Symlink(outName, fname)
}

func dumpVendorToFile(vendor mtgban.Vendor, fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer file.Close()

	return mtgban.WriteVendorToJSON(vendor, file)
}

func untangleMarket(init bool, currentDir string, newbc *mtgban.BanClient, scraper mtgban.Market, key string) error {
	names := ScraperOptions[key].Keepers
	log.Println("Untangling", scraper.Info().Shorthand, "to", names)

	for _, name := range names {
		ScraperMap[name] = key
		ScraperNames[name] = name
	}
	var needsLoading bool

	// Try reading from the two sub seller files (the main file is not dumped)
	if init {
		// Both files need to be present
		ok := true
		for _, name := range names {
			subfname := path.Join(InventoryDir, name+"-latest.json")
			if !fileExists(subfname) {
				ok = false
				break
			}
		}

		if ok {
			for _, name := range names {
				subfname := path.Join(InventoryDir, name+"-latest.json")

				seller, err := loadInventoryFromFile(subfname)
				if err != nil {
					return err
				}

				// Register so that it will be added to the main Sellers array
				newbc.Register(seller)

				inv, _ := seller.Inventory()
				log.Printf("Loaded from file with %d entries", len(inv))
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
			if SliceStringHas(names, seller.Info().Shorthand) && time.Now().Sub(*seller.Info().InventoryTimestamp) < SkipRefreshCooldown {
				log.Println("Trying to skip", seller.Info().Name, seller.Info().Shorthand, "because too recent")
			} else {
				needsLoading = true
			}
		}
	}

	if needsLoading {
		log.Println("Loading from Market scraper")
		start := time.Now()

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
		log.Println("Took", time.Now().Sub(start))

		// Split subsellers
		sellers, err := mtgban.Seller2Sellers(scraper)
		if err != nil {
			return err
		}

		// Dump files for the requested sellers
		for _, seller := range sellers {
			if SliceStringHas(names, seller.Info().Shorthand) {
				// Add selected seller to the future global seller map
				newbc.Register(seller)

				fname := path.Join(InventoryDir, seller.Info().Shorthand+"-latest.json")

				err = dumpInventoryToFile(seller, currentDir, fname)
				if err != nil {
					log.Println(err)
					continue
				}
				ScraperOptions[key].Logger.Println(seller.Info().Name, "saved to file")

				targetDir := path.Join(InventoryDir, time.Now().Format("2006-01-02/15"))
				err = uploadSeller(seller, targetDir)
				if err != nil {
					log.Println(err)
					continue
				}
				ScraperOptions[key].Logger.Println(seller.Info().Name, "uploaded to the cloud")
			}
		}

		// Stash to redis if requested
		if ScraperOptions[key].StashMarkets {
			for _, seller := range sellers {
				db, found := ScraperOptions[key].RDBs[seller.Info().Shorthand]
				if !found {
					continue
				}

				start := time.Now()
				log.Printf("Stashing %s inventory data to DB", seller.Info().Shorthand)
				inv, _ := seller.Inventory()
				key := seller.Info().InventoryTimestamp.Format("2006-01-02")
				for uuid, entries := range inv {
					err = db.HSet(context.Background(), uuid, key, entries[0].Price).Err()
					if err != nil {
						ServerNotify("redis", err.Error())
						break
					}
				}
				log.Println("Took", time.Now().Sub(start))
			}
		}

		log.Println("-- OK")
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

// Map of indices for all scrapers stashed in the db
var DBs = map[string]int{
	"ck_retail":     0,
	"ck_buylist":    1,
	"tcg_low":       2,
	"tcg_market":    3,
	"mkm_low":       4,
	"mkm_trend":     5,
	"starcitygames": 6,
	"abugames":      7,
}

var ScraperOptions = map[string]*scraperOption{
	"abugames": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := abugames.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
		StashBuylist: true,
		RDBs: map[string]*redis.Client{
			"buylist": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["abugames"],
			}),
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
		StashInventory: true,
		StashBuylist:   true,
		RDBs: map[string]*redis.Client{
			"retail": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["ck_retail"],
			}),
			"buylist": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["ck_buylist"],
			}),
		},
	},
	"coolstuffinc": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := coolstuffinc.NewScraperOfficial(Config.Api["csi_token"])
			scraper.LogCallback = logger.Printf
			scraper.Partner = Config.Affiliate["CSI"]
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
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := starcitygames.NewScraper(Config.Api["scg_username"], Config.Api["scg_password"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
		StashBuylist: true,
		RDBs: map[string]*redis.Client{
			"buylist": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["starcitygames"],
			}),
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
		Keepers: []string{
			TCG_LOW,
			TCG_MARKET,
			TCG_DIRECT_LOW,
		},
		StashMarkets: true,
		RDBs: map[string]*redis.Client{
			TCG_LOW: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["tcg_low"],
			}),
			TCG_MARKET: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["tcg_market"],
			}),
		},
	},
	"magiccorner": &scraperOption{
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
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := cardmarket.NewScraperIndex(Config.Api["mkm_app_token"], Config.Api["mkm_app_secret"])
			if err != nil {
				return nil, err
			}
			scraper.Affiliate = Config.Affiliate["MKM"]
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
		Keepers:      []string{MKM_LOW, MKM_TREND},
		StashMarkets: true,
		RDBs: map[string]*redis.Client{
			MKM_LOW: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["mkm_low"],
			}),
			MKM_TREND: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   DBs["mkm_trend"],
			}),
		},
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
			scraper, err := cardtrader.NewScraperMarket(Config.Api["cardtrader"])
			if err != nil {
				return nil, err
			}
			scraper.LogCallback = logger.Printf
			scraper.ShareCode = Config.Affiliate["CT"]
			return scraper, nil
		},
		Keepers: []string{
			CT_STANDARD,
			CT_ZERO,
		},
	},
	"mtgseattle": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := mtgseattle.NewScraper()
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
	"cardkingdom_sealed": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := cardkingdom.NewScraperSealed()
			scraper.LogCallback = logger.Printf
			scraper.Partner = Config.Affiliate["CK"]
			return scraper, nil
		},
		StashInventory: true,
		StashBuylist:   true,
		RDBs: map[string]*redis.Client{
			"retail": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   0,
			}),
			"buylist": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   1,
			}),
		},
	},
	"tcg_sealed": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := tcgplayer.NewScraperSealed(Config.Api["tcg_public"], Config.Api["tcg_private"])
			scraper.Affiliate = Config.Affiliate["TCG"]
			scraper.LogCallback = logger.Printf
			scraper.MaxConcurrency = 4
			return scraper, nil
		},
		StashInventory: true,
		RDBs: map[string]*redis.Client{
			"retail": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   2,
			}),
		},
	},
	"cardmarket_sealed": &scraperOption{
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper, err := cardmarket.NewScraperSealed(Config.Api["mkm_app_token"], Config.Api["mkm_app_secret"])
			if err != nil {
				return nil, err
			}
			scraper.Affiliate = Config.Affiliate["MKM"]
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
		StashInventory: true,
		RDBs: map[string]*redis.Client{
			"retail": redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   4,
			}),
		},
	},
	"toamagic": &scraperOption{
		OnlyVendor: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := toamagic.NewScraper()
			scraper.LogCallback = logger.Printf
			return scraper, nil
		},
	},
	"tcg_direct_net": &scraperOption{
		DevEnabled: true,
		Init: func(logger *log.Logger) (mtgban.Scraper, error) {
			scraper := tcgplayer.NewTCGDirectNet()
			return scraper, nil
		},
	},
}

// Associate Scraper shorthands to ScraperOptions keys
var ScraperMap map[string]string

// Assiciate Scraper shorthands to Scraper Names
var ScraperNames map[string]string

// A default scale for converting non-NM prices to NM
var defaultGradeMap = map[string]float64{
	"NM": 1, "SP": 1.25, "MP": 1.67, "HP": 2.5, "PO": 4,
}

// Create log and ScraperMap
func loadOptions() {
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
				OldVersions: 1,
			})
			if err != nil {
				log.Printf("Failed to create logFile for %s: %s", key, err)
				opt.Logger = log.New(os.Stderr, "", log.LstdFlags)
				continue
			}
			opt.Logger = log.New(logFile, "", log.LstdFlags)
		}

		scraper, err := opt.Init(opt.Logger)
		if err != nil {
			continue
		}

		// Custom untangling
		for _, name := range opt.Keepers {
			ScraperMap[name] = key
		}

		ScraperMap[scraper.Info().Shorthand] = key
	}
}

func loadScrapers() {
	init := !DatabaseLoaded
	if init {
		ServerNotify("init", "loading started")
	} else {
		ServerNotify("refresh", "full refresh started")
	}

	currentDir := path.Join(InventoryDir, fmt.Sprintf("%03d", time.Now().YearDay()))
	mkDirIfNotExisting(currentDir)

	newbc := mtgban.NewClient()

	// Keep track of the names used in the options table, so that we can
	// reference the mutex more freely
	if ScraperMap == nil {
		ScraperMap = map[string]string{}
	}
	if ScraperNames == nil {
		ScraperNames = map[string]string{}
	}

	loadOptions()

	for key, opt := range ScraperOptions {
		if DevMode && !opt.DevEnabled {
			continue
		}

		log.Println("Initializing " + key)
		scraper, err := opt.Init(opt.Logger)
		if err != nil {
			msg := fmt.Sprintf("error initializing %s: %s", key, err.Error())
			ServerNotify("init", msg, true)
			continue
		}

		if len(opt.Keepers) != 0 {
			if !opt.OnlyVendor {
				err := untangleMarket(init, currentDir, newbc, scraper.(mtgban.Market), key)
				if err != nil {
					msg := fmt.Sprintf("failed to load %s: %s", key, err.Error())
					ServerNotify("init", msg, true)
					// Use the old data instead of skipping it
					if !init {
						for _, seller := range Sellers {
							if seller != nil && SliceStringHas(ScraperOptions[key].Keepers, seller.Info().Shorthand) {
								newbc.RegisterSeller(seller)
							}
						}
					}
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
		ScraperNames[scraper.Info().Shorthand] = scraper.Info().Name
	}

	// Sort the sellers/vendors arrays by name
	//
	// Note that pointers are shared between these two arrays,
	// things like Price Ratio (bl data depending on inv data)
	// still work just fine, even if we don't use them in the
	// global arrays in the end.
	newSellers := newbc.Sellers()
	sort.Slice(newSellers, func(i, j int) bool {
		if newSellers[i].Info().Name == newSellers[j].Info().Name {
			return newSellers[i].Info().Shorthand < newSellers[j].Info().Shorthand
		}
		return newSellers[i].Info().Name < newSellers[j].Info().Name
	})
	newVendors := newbc.Vendors()
	sort.Slice(newVendors, func(i, j int) bool {
		if newVendors[i].Info().Name == newVendors[j].Info().Name {
			return newVendors[i].Info().Shorthand < newVendors[j].Info().Shorthand
		}
		return newVendors[i].Info().Name < newVendors[j].Info().Name
	})

	// Allocate enough space for the global pointers
	if Sellers == nil {
		Sellers = make([]mtgban.Seller, len(newSellers))
	}
	if Vendors == nil {
		Vendors = make([]mtgban.Vendor, len(newVendors))
	}

	log.Println("Sellers table")
	var msgS string
	for i := range newSellers {
		msgS += fmt.Sprintf("%d ", i)
		if newSellers[i] == nil {
			msgS += "<nil>\n"
			continue
		}
		msgS += fmt.Sprintf("%s %s\n", newSellers[i].Info().Name, newSellers[i].Info().Shorthand)
	}
	ServerNotify("init", msgS)
	loadSellers(newSellers)

	loadTCGDirectNet(newVendors)

	log.Println("Vendors table")
	var msgV string
	for i := range newVendors {
		msgV += fmt.Sprintf("%d ", i)
		if newVendors[i] == nil {
			msgV += "<nil>\n"
			continue
		}
		msgV += fmt.Sprintf("%s %s\n", newVendors[i].Info().Name, newVendors[i].Info().Shorthand)
	}
	ServerNotify("init", msgV)
	loadVendors(newVendors)

	if BenchMode {
		return
	}

	updateStaticData()

	if init {
		ServerNotify("init", "loading completed")
	} else {
		ServerNotify("refresh", "full refresh completed")
	}
}

func updateStaticData() {
	if Infos == nil {
		Infos = map[string]mtgban.InventoryRecord{}
	}

	SealedEditionsSorted, SealedEditionsList = getSealedEditions()
	AllEditionsKeys, AllEditionsMap = getAllEditions()
	TreeEditionsKeys, TreeEditionsMap = getTreeEditions()

	TotalSets = len(AllEditionsKeys)
	TotalUnique = len(mtgmatcher.GetUUIDs())
	var totalCards int
	for _, key := range AllEditionsKeys {
		totalCards += AllEditionsMap[key].Size
	}
	TotalCards = totalCards

	go loadInfos()
	go runSealedAnalysis()

	// Load prices for API users
	if !DevMode {
		go prepareCKAPI()
	}

	LastUpdate = time.Now().Format(time.RFC3339)
}

func loadSellers(newSellers []mtgban.Seller) {
	defer recoverPanicScraper()

	init := !DatabaseLoaded
	currentDir := path.Join(InventoryDir, fmt.Sprintf("%03d", time.Now().YearDay()))
	mkDirIfNotExisting(currentDir)

	// Load Sellers
	for i := range newSellers {
		log.Println(newSellers[i].Info().Name, newSellers[i].Info().Shorthand, "Inventory")

		fname := path.Join(InventoryDir, newSellers[i].Info().Shorthand+"-latest.json")
		if init && fileExists(fname) {
			seller, err := loadInventoryFromFile(fname)
			if err != nil {
				log.Println(err)
				continue
			}
			Sellers[i] = seller

			inv, _ := seller.Inventory()
			log.Printf("Loaded from file with %d entries", len(inv))
		} else {
			_, ok := newSellers[i].(mtgban.Scraper).(mtgban.Market)
			if ok {
				log.Println("Already loaded during untangling")
				continue
			}

			opts := ScraperOptions[ScraperMap[newSellers[i].Info().Shorthand]]

			// If the old scraper data is old enough, pull from the new scraper
			// and update it in the global slice
			if Sellers[i] == nil || time.Now().Sub(*Sellers[i].Info().InventoryTimestamp) > SkipRefreshCooldown {
				ServerNotify("reload", "Loading from seller "+newSellers[i].Info().Shorthand)
				start := time.Now()
				err := updateSellerAtPosition(newSellers[i], i, true)
				if err != nil {
					msg := fmt.Sprintf("seller %s %s - %s", newSellers[i].Info().Name, newSellers[i].Info().Shorthand, err.Error())
					ServerNotify("reload", msg, true)
					continue
				}
				log.Println("Took", time.Now().Sub(start))
			}

			// Stash data to DB if requested
			if opts.StashInventory {
				start := time.Now()
				log.Println("Stashing", Sellers[i].Info().Name, Sellers[i].Info().Shorthand, "inventory data to DB")
				inv, _ := Sellers[i].Inventory()

				key := Sellers[i].Info().InventoryTimestamp.Format("2006-01-02")
				for uuid, entries := range inv {
					// Adjust price through defaultGradeMap in case NM is not available
					price := entries[0].Price * defaultGradeMap[entries[0].Conditions]
					// Use NX because the price might have already been set using more accurate
					// information (instead of the derivation above)
					err := opts.RDBs["retail"].HSetNX(context.Background(), uuid, key, price).Err()
					if err != nil {
						ServerNotify("redis", err.Error())
						break
					}
				}
				log.Println("Took", time.Now().Sub(start))
			}

			err := dumpInventoryToFile(Sellers[i], currentDir, fname)
			if err != nil {
				log.Println(err)
				continue
			}
			opts.Logger.Println("Saved to file")

			targetDir := path.Join(InventoryDir, time.Now().Format("2006-01-02/15"))
			err = uploadSeller(Sellers[i], targetDir)
			if err != nil {
				log.Println(err)
				continue
			}
			opts.Logger.Println("Uploaded to the cloud")
		}
		log.Println("-- OK")
	}
}

func loadVendors(newVendors []mtgban.Vendor) {
	defer recoverPanicScraper()

	init := !DatabaseLoaded
	currentDir := path.Join(BuylistDir, fmt.Sprintf("%03d", time.Now().YearDay()))
	mkDirIfNotExisting(currentDir)

	// Load Vendors
	for i := range newVendors {
		log.Println(newVendors[i].Info().Name, newVendors[i].Info().Shorthand, "Buylist")

		fname := path.Join(BuylistDir, newVendors[i].Info().Shorthand+"-latest.json")
		if init && fileExists(fname) {
			vendor, err := loadBuylistFromFile(fname)
			if err != nil {
				log.Println(err)
				continue
			}
			Vendors[i] = vendor

			bl, _ := vendor.Buylist()
			log.Printf("Loaded from file with %d entries", len(bl))
		} else {
			opts := ScraperOptions[ScraperMap[newVendors[i].Info().Shorthand]]

			// If the old scraper data is old enough, pull from the new scraper
			// and update it in the global slice
			if Vendors[i] == nil || time.Now().Sub(*Vendors[i].Info().BuylistTimestamp) > SkipRefreshCooldown {
				ServerNotify("reload", "Loading from vendor "+newVendors[i].Info().Shorthand)
				start := time.Now()
				err := updateVendorAtPosition(newVendors[i], i, true)
				if err != nil {
					msg := fmt.Sprintf("vendor %s %s - %s", newVendors[i].Info().Name, newVendors[i].Info().Shorthand, err.Error())
					ServerNotify("reload", msg, true)
					continue
				}
				log.Println("Took", time.Now().Sub(start))
			}

			// Stash data to DB if requested
			if opts.StashBuylist {
				start := time.Now()
				log.Println("Stashing", Vendors[i].Info().Name, Vendors[i].Info().Shorthand, "buylist data to DB")
				bl, _ := Vendors[i].Buylist()
				key := Vendors[i].Info().BuylistTimestamp.Format("2006-01-02")
				for uuid, entries := range bl {
					err := opts.RDBs["buylist"].HSet(context.Background(), uuid, key, entries[0].BuyPrice).Err()
					if err != nil {
						ServerNotify("redis", err.Error())
						break
					}
				}
				log.Println("Took", time.Now().Sub(start))
			}

			err := dumpBuylistToFile(Vendors[i], currentDir, fname)
			if err != nil {
				log.Println(err)
				continue
			}
			opts.Logger.Println("Saved to file")

			targetDir := path.Join(BuylistDir, time.Now().Format("2006-01-02/15"))
			err = uploadVendor(Vendors[i], targetDir)
			if err != nil {
				log.Println(err)
				continue
			}
			opts.Logger.Println("Uploaded to the cloud")
		}
		log.Println("-- OK")
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

func recoverPanicScraper() {
	errPanic := recover()
	if errPanic != nil {
		log.Println("panic occurred:", errPanic)

		// Restrict stack size to fit into discord message
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		if len(buf) > 1024 {
			buf = buf[:1024]
		}

		var msg string
		err, ok := errPanic.(error)
		if ok {
			msg = err.Error()
		} else {
			msg = "unknown error"
		}
		ServerNotify("panic", msg, true)
		ServerNotify("panic", string(buf))

		return
	}
}

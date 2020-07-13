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

func specialTCGhandle(init bool, currentDir string, newbc *mtgban.BanClient, tcg *tcgplayer.TCGPlayerMarket) error {
	dirName := path.Clean(currentDir+"/..") + "/"

	// Check if both sub seller files are present
	lowname := dirName + "TCG Low-latest.csv"
	lowdirectname := dirName + "TCG Direct Low-latest.csv"
	if init && fileExists(lowname) && fileExists(lowdirectname) {
		log.Println("Found TCG subseller files")

		for _, name := range []string{"TCG Low", "TCG Direct Low"} {
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
		if seller.Info().Name == "TCG Low" || seller.Info().Name == "TCG Direct Low" {
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

func trySCGScraper() mtgban.Vendor {
	resp, err := http.Get(Config.Api["scg_categories"])
	if err != nil {
		log.Println("SCG", err)
		return nil
	}
	defer resp.Body.Close()

	scg, err := starcitygames.NewScraper(resp.Body)
	if err != nil {
		log.Println("SCG", err)
		return nil
	}

	return scg
}

func loadScrapers() {
	init := !DatabaseLoaded
	if init {
		log.Println("Loading data")
	} else {
		log.Println("Updating data")
	}

	newbc := mtgban.NewClient()

	newck := cardkingdom.NewScraper()
	newck.Partner = Config.Affiliate["CK"]
	newck.LogCallback = log.Printf

	newsz := strikezone.NewScraper()
	newsz.LogCallback = log.Printf

	newabu := abugames.NewScraper()
	newabu.LogCallback = log.Printf

	newcfb := channelfireball.NewScraper()
	newcfb.LogCallback = log.Printf
	newcfb.MaxConcurrency = 6

	newmm := miniaturemarket.NewScraper()
	newmm.LogCallback = log.Printf

	new95 := ninetyfive.NewScraper()
	new95.LogCallback = log.Printf

	tcg := tcgplayer.NewScraperMarket(Config.Api["tcg_public"], Config.Api["tcg_private"])
	tcg.Affiliate = Config.Affiliate["TCG"]
	tcg.LogCallback = log.Printf

	newcsi := coolstuffinc.NewScraper()
	newcsi.LogCallback = log.Printf
	newcfb.MaxConcurrency = 6

	newftf, _ := facetoface.NewScraper()
	if newftf != nil {
		newftf.LogCallback = log.Printf
	}

	newtat := trollandtoad.NewScraper()
	newtat.LogCallback = log.Printf

	dirName := "cache_inv/"
	currentDir := fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
	mkDirIfNotExisting(currentDir)

	newbc.Register(newck)
	newbc.Register(newsz)
	newbc.Register(new95)
	if !DevMode {
		newbc.Register(newabu)
		newbc.Register(newcfb)
		newbc.Register(newmm)
		newbc.Register(newcsi)
		newbc.RegisterVendor(tcg)
		newbc.Register(newtat)

		if newftf != nil {
			newbc.Register(newftf)
		}
		newscg := trySCGScraper()
		if newscg != nil {
			newbc.RegisterVendor(newscg)
		}

		err := specialTCGhandle(init, currentDir, newbc, tcg)
		if err != nil {
			log.Println(err)
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

	// Chand destination directory
	dirName = "cache_bl/"
	currentDir = fmt.Sprintf("%s%03d", dirName, time.Now().YearDay())
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

	LastUpdate = time.Now()

	log.Println("Scrapers loaded")
}

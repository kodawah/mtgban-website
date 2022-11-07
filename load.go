package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/kodabb/go-mtgban/mtgban"
)

type ScraperConfig struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Path      string `json:"path"`
}

// Retrieve the list of Scrapers and their configuration
func downloadScrapersConfig(path string) (map[string]*ScraperConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	rc, err := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(path).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var config map[string]*ScraperConfig
	err = json.NewDecoder(rc).Decode(&config)
	return config, err
}

func uploadScrapersConfig(config map[string]*ScraperConfig, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	wc := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(path).NewWriter(ctx)
	wc.ContentType = "application/json"
	defer wc.Close()

	return json.NewEncoder(wc).Encode(&config)
}

// Convert from map to a sorted array
func configMap2configArray(configMap map[string]*ScraperConfig) []ScraperConfig {
	var configs []ScraperConfig
	for _, config := range configMap {
		configs = append(configs, *config)
	}
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].Name == configs[j].Name {
			return configs[i].Shorthand < configs[j].Shorthand
		}
		return configs[i].Name < configs[j].Name
	})
	return configs
}

var configMutex sync.RWMutex
var SellersConfigMap map[string]*ScraperConfig
var VendorsConfigMap map[string]*ScraperConfig

func loadScrapersNG() error {
	init := !DatabaseLoaded
	if init {
		ServerNotify("init", "loading started")
		ScraperNames = map[string]string{}
	} else {
		ServerNotify("refresh", "full refresh started")
	}

	var err error
	if SellersConfigMap == nil {
		SellersConfigMap, err = downloadScrapersConfig("sellers.json")
		if err != nil {
			return err
		}
	}
	if VendorsConfigMap == nil {
		VendorsConfigMap, err = downloadScrapersConfig("vendors.json")
		if err != nil {
			return err
		}
	}
	configMutex.RLock()
	sellersConfig := configMap2configArray(SellersConfigMap)
	vendorsConfig := configMap2configArray(VendorsConfigMap)
	configMutex.RUnlock()

	var sellers []mtgban.Seller
	var vendors []mtgban.Vendor

	// If booting up, preload with the cache
	if init {
		for _, scraper := range sellersConfig {
			ScraperNames[scraper.Shorthand] = scraper.Name

			log.Printf("Loading %s inventory from cache", scraper.Shorthand)
			fname := path.Join(InventoryDir, scraper.Shorthand+".json")
			seller, err := loadSellerFromFile(fname)
			if err != nil {
				log.Println(err)
				continue
			}
			inv, err := seller.Inventory()
			if err != nil || len(inv) == 0 {
				log.Printf("-- ERROR (%v) no entries", err)
				continue
			}
			log.Printf("-- OK: %d entries", len(inv))
			sellers = append(sellers, seller)
		}
		for _, scraper := range vendorsConfig {
			ScraperNames[scraper.Shorthand] = scraper.Name

			log.Printf("Loading %s buylist from cache", scraper.Shorthand)
			fname := path.Join(BuylistDir, scraper.Shorthand+".json")
			vendor, err := loadVendorFromFile(fname)
			if err != nil {
				log.Println(err)
				continue
			}
			bl, err := vendor.Buylist()
			if err != nil || len(bl) == 0 {
				log.Printf("-- ERROR (%v) no entries", err)
				continue
			}

			log.Printf("-- OK: %d entries", len(bl))
			vendors = append(vendors, vendor)
		}
		Sellers = sellers
		Vendors = vendors

		log.Printf("Loaded %d sellers and %d vendors from cache", len(sellers), len(vendors))
	}

	if !SkipInitialRefresh {
		go refreshSellerCache()
		go refreshVendorCache()
	}

	updateStaticData()
	loadOptions()

	return nil
}

func refreshSellerCache() {
	log.Println("Loading sellers from the cloud")

	configMutex.RLock()
	configs := configMap2configArray(SellersConfigMap)
	configMutex.RUnlock()

	sellers, err := downloadSellers(configs)
	if err != nil {
		ServerNotify("refresh", fmt.Sprintf("unable to refresh sellers: %v", err))
		return
	}
	Sellers = sellers

	log.Printf("Loaded %d sellers from the cloud", len(sellers))
}

func downloadSellers(configs []ScraperConfig) ([]mtgban.Seller, error) {
	var sellers []mtgban.Seller

	for _, config := range configs {
		log.Println("Downloading", config.Path)
		start := time.Now()

		seller, err := downloadSeller(config.Path)
		if err != nil {
			return nil, err
		}
		sellers = append(sellers, seller)

		log.Println(config.Shorthand, "took", time.Since(start))

		// Cache the obtained data
		fname := path.Join(InventoryDir, config.Shorthand+".json")
		err = dumpSellerToFile(seller, fname)
		if err != nil {
			return nil, err
		}
	}
	return sellers, nil
}

func downloadSeller(path string) (mtgban.Seller, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	rc, err := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(path).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return mtgban.ReadSellerFromJSON(rc)
}

func refreshVendorCache() {
	log.Println("Loading vendors from the cloud")

	configMutex.RLock()
	configs := configMap2configArray(VendorsConfigMap)
	configMutex.RUnlock()

	vendors, err := downloadVendors(configs)
	if err != nil {
		ServerNotify("refresh", fmt.Sprintf("unable to refresh vendors: %v", err))
		return
	}
	Vendors = vendors

	log.Printf("Loaded %d vendors from the cloud", len(vendors))
}

func downloadVendors(configs []ScraperConfig) ([]mtgban.Vendor, error) {
	var vendors []mtgban.Vendor

	for _, config := range configs {
		log.Println("Downloading", config.Path)

		start := time.Now()

		vendor, err := downloadVendor(config.Path)
		if err != nil {
			return nil, err
		}
		vendors = append(vendors, vendor)

		log.Println(config.Shorthand, "took", time.Since(start))

		// Cache the obtained data
		fname := path.Join(BuylistDir, config.Shorthand+".json")
		err = dumpVendorToFile(vendor, fname)
		if err != nil {
			return nil, err
		}
	}
	return vendors, nil
}

func downloadVendor(path string) (mtgban.Vendor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	rc, err := GCSBucketClient.Bucket(Config.Uploader.BucketName).Object(path).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return mtgban.ReadVendorFromJSON(rc)
}

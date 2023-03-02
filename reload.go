package main

import (
	"errors"
	"path"
	"time"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/tcgplayer"
)

func updateSellerAtPosition(seller mtgban.Seller, i int, andLock bool) error {
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
		return err
	}
	if len(inv) == 0 {
		return errors.New("empty inventory")
	}

	// Save seller in global array, making sure it's _only_ a Seller
	// and not anything esle, so that filtering works like expected
	Sellers[i] = mtgban.NewSellerFromInventory(inv, seller.Info())

	targetDir := path.Join(InventoryDir, time.Now().Format("2006-01-02/15"))
	go uploadSeller(Sellers[i], targetDir)
	return nil
}

func updateVendorAtPosition(vendor mtgban.Vendor, i int, andLock bool) error {
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
		return err
	}
	if len(bl) == 0 {
		return errors.New("empty buylist")
	}

	// Save vendor in global array, making sure it's _only_ a Vendor
	// and not anything esle, so that filtering works like expected
	Vendors[i] = mtgban.NewVendorFromBuylist(bl, vendor.Info())

	targetDir := path.Join(BuylistDir, time.Now().Format("2006-01-02/15"))
	go uploadVendor(Vendors[i], targetDir)
	return nil
}

// Load the fake buylist from the TCG Direct data
func loadTCGDirectNet(newVendors []mtgban.Vendor) {
	for _, seller := range Sellers {
		if seller != nil && seller.Info().Name == TCG_DIRECT {
			for i := range newVendors {
				if newVendors[i] != nil && newVendors[i].Info().Name == TCG_DIRECT_NET {
					tcg := tcgplayer.NewTCGDirectNet()
					tcg.DirectInventory, _ = seller.Inventory()
					// No error possible
					tcg.Buylist()
					// Replace the vendor
					newVendors[i] = tcg
					break
				}
			}
			break
		}
	}
}

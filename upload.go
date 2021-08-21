package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	MaxUploadEntries = 100

	TooManyEntriesMessage = "Note that this tool supports a maximum of 100 entries at a time"
)

type UploadEntry struct {
	Card          mtgmatcher.Card
	CardId        string
	MismatchError error
	OriginalPrice float64
}

func Upload(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Upload", sig)

	// Max file size is 10 MB
	r.ParseMultipartForm(10 << 20)

	// Check cookies to set preferences
	blMode := readSetFlag(w, r, "mode", "uploadMode")
	pageVars.IsBuylist = blMode

	// FormFile returns the first file for the given key `myFile`
	// it also returns the FileHeader so we can get the Filename,
	// the Header and the size of the file
	file, handler, err := r.FormFile("cardListFile")
	if err != nil {
		render(w, "upload.html", pageVars)
		return
	}
	defer file.Close()

	log.Printf("Buylist mode: %+v", blMode)
	log.Printf("Uploaded File: %+v", handler.Filename)
	log.Printf("File Size: %+v bytes", handler.Size)
	log.Printf("MIME Header: %+v", handler.Header)

	blocklistRetail, blocklistBuylist := getDefaultBlocklists(sig)
	var enabledStores []string

	if blMode {
		for _, vendor := range Vendors {
			if vendor != nil && !SliceStringHas(blocklistBuylist, vendor.Info().Shorthand) && !vendor.Info().SealedMode {
				enabledStores = append(enabledStores, vendor.Info().Shorthand)
			}
		}
	} else {
		for _, seller := range Sellers {
			if seller != nil && !SliceStringHas(blocklistRetail, seller.Info().Shorthand) && !seller.Info().SealedMode {
				enabledStores = append(enabledStores, seller.Info().Shorthand)
			}
		}
	}

	start := time.Now()

	// Load data
	uploadedData, err := loadCsv(file)
	if err != nil {
		pageVars.WarningMessage = err.Error()
		render(w, "upload.html", pageVars)
		return
	}

	// Extract card Ids
	cardIds := make([]string, 0, len(uploadedData))
	for i := range uploadedData {
		cardIds = append(cardIds, uploadedData[i].CardId)
	}

	// Check not too many entries got uploaded
	if len(cardIds) >= MaxUploadEntries {
		pageVars.InfoMessage = TooManyEntriesMessage
	}

	// Search
	var results map[string]map[string]*BanPrice
	if blMode {
		results = getVendorPrices("", enabledStores, "", "", cardIds, false, false)
	} else {
		results = getSellerPrices("", enabledStores, "", "", cardIds, false, false)
	}

	pageVars.Metadata = map[string]GenericCard{}
	pageVars.SearchQuery = handler.Filename
	pageVars.ScraperKeys = enabledStores
	pageVars.CompactEntries = results
	pageVars.UploadEntries = uploadedData
	pageVars.TotalEntries = map[string]float64{}

	for _, stores := range results {
		for shorthand, price := range stores {
			if price == nil {
				continue
			}
			if price.Regular != 0 {
				pageVars.TotalEntries[shorthand] += price.Regular
			} else {
				pageVars.TotalEntries[shorthand] += price.Foil
			}
		}
	}

	// Load up image links
	for _, data := range uploadedData {
		if data.MismatchError != nil {
			continue
		}

		_, found := pageVars.Metadata[data.CardId]
		if !found {
			pageVars.Metadata[data.CardId] = uuid2card(data.CardId, true, false)
		}
		if pageVars.Metadata[data.CardId].Reserved {
			pageVars.HasReserved = true
		}
		if pageVars.Metadata[data.CardId].Stocks {
			pageVars.HasStocks = true
		}
	}

	// Logs
	user := GetParamFromSig(sig, "UserEmail")
	msgMode := "retail"
	if blMode {
		msgMode = "buylist"
	}
	msg := fmt.Sprintf("%s uploaded %d %s entries from %s, took %v", user, len(cardIds), msgMode, handler.Filename, time.Since(start))
	Notify("upload", msg)
	LogPages["Upload"].Println(msg)
	if DevMode {
		log.Println(msg)
	}

	// Touchdown!
	render(w, "upload.html", pageVars)
}

func loadCsv(reader io.Reader) ([]UploadEntry, error) {
	csvReader := csv.NewReader(reader)

	// Load header
	first, err := csvReader.Read()
	if err == io.EOF {
		return nil, errors.New("empty input file")
	}
	if err != nil {
		log.Println("Error reading header: %v", err)
		return nil, errors.New("error reading file header")
	}

	if len(first) < 2 {
		log.Println("Too few fields: %v", first)
		return nil, errors.New("too few fields")
	}

	// Set some default values for the mandatory fields
	indexMap := map[string]int{
		"cardName": 0,
		"edition":  1,
	}

	// Parse the header to understand where these fields are
	for i, field := range first {
		field = strings.ToLower(field)
		switch {
		case field == "id" || (strings.Contains(field, "id") && (strings.Contains(field, "tcg") || strings.Contains(field, "scyfall"))):
			_, found := indexMap["id"]
			if !found {
				indexMap["id"] = i
			}
		case strings.Contains(field, "name") && !strings.Contains(field, "edition") && !strings.Contains(field, "set"):
			indexMap["cardName"] = i
		case strings.Contains(field, "edition") || strings.Contains(field, "set"):
			indexMap["edition"] = i
		case strings.Contains(field, "number") || strings.Contains(field, "variant") || strings.Contains(field, "variation"):
			_, found := indexMap["variant"]
			if !found {
				indexMap["variant"] = i
			}
		case strings.Contains(field, "foil") || strings.Contains(field, "printing") || field == "f/nf" || field == "nf/f":
			_, found := indexMap["printing"]
			if !found {
				indexMap["printing"] = i
			}
		case strings.Contains(field, "price"):
			_, found := indexMap["price"]
			if !found {
				indexMap["price"] = i
			}
		}
	}

	foundHashes := map[string]bool{}
	var i int
	var uploadEntries []UploadEntry
	for {
		i++
		if i > MaxUploadEntries {
			break
		}

		res := UploadEntry{}
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			res.MismatchError = err
			uploadEntries = append(uploadEntries, res)
			continue
		}

		_, found := indexMap["id"]
		if found {
			res.Card.Id = record[indexMap["id"]]
		}

		res.Card.Name = record[indexMap["cardName"]]
		res.Card.Edition = record[indexMap["edition"]]

		_, found = indexMap["variant"]
		if found {
			res.Card.Variation = record[indexMap["variant"]]
		}

		printing := strings.ToLower(record[indexMap["printing"]])
		if printing == "y" || printing == "yes" || printing == "true" ||
			mtgmatcher.Contains(printing, "foil") ||
			mtgmatcher.Contains(res.Card.Variation, "foil") {
			res.Card.Foil = true
		}

		_, found = indexMap["price"]
		if found {
			res.OriginalPrice, _ = strconv.ParseFloat(record[indexMap["price"]], 64)
		}

		res.CardId, err = mtgmatcher.Match(&res.Card)

		// Skip repeated entries
		if foundHashes[res.CardId] {
			continue
		}

		// Report any errors to the user or track which hash was found
		if err != nil {
			res.MismatchError = fmt.Errorf("record on line %d: %s", i+1, err.Error())
		} else {
			foundHashes[res.CardId] = true
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

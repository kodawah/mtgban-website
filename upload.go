package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize/v2"
	"github.com/extrame/xls"
	"gopkg.in/Iwark/spreadsheet.v2"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	MaxUploadEntries = 150

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

	// Disable buylist if not permitted
	canBuylist, _ := strconv.ParseBool(GetParamFromSig(sig, "UploadBuylistEnabled"))
	if !canBuylist {
		blMode = false
	}

	// Set flags needed to show elements on the page ui
	pageVars.IsBuylist = blMode
	pageVars.CanBuylist = canBuylist

	blocklistRetail, blocklistBuylist := getDefaultBlocklists(sig)
	var enabledStores []string
	var allSellers []string
	var allVendors []string

	// Load all possible sellers, and vendors according to user permissions
	for _, seller := range Sellers {
		if seller != nil && !SliceStringHas(blocklistRetail, seller.Info().Shorthand) && !seller.Info().SealedMode && !seller.Info().MetadataOnly {
			allSellers = append(allSellers, seller.Info().Shorthand)
		}
	}
	for _, vendor := range Vendors {
		if vendor != nil && !SliceStringHas(blocklistBuylist, vendor.Info().Shorthand) && !vendor.Info().SealedMode {
			allVendors = append(allVendors, vendor.Info().Shorthand)
		}
	}

	// Set the store names for the <select> box
	pageVars.SellerKeys = allSellers
	pageVars.VendorKeys = allVendors

	// Load the preferred list of enabled stores for the <select> box
	// The first check is for when the cookie is not yet set
	enabledSellers := readCookie(r, "enabledSellers")
	if len(enabledSellers) == 0 {
		pageVars.EnabledSellers = Config.AffiliatesList
	} else {
		pageVars.EnabledSellers = strings.Split(enabledSellers, "|")
	}
	enabledVendors := readCookie(r, "enabledVendors")
	if len(enabledVendors) == 0 {
		pageVars.EnabledVendors = allVendors
	} else {
		pageVars.EnabledVendors = strings.Split(enabledVendors, "|")
	}

	cachedGdocURL := readCookie(r, "gdocURL")
	pageVars.RemoteLinkURL = cachedGdocURL

	// Filter out any unselected store from the full list
	stores := r.Form["stores"]
	if blMode {
		for _, store := range stores {
			if SliceStringHas(allVendors, store) {
				enabledStores = append(enabledStores, store)
			}
		}
	} else {
		for _, store := range stores {
			if SliceStringHas(allSellers, store) {
				enabledStores = append(enabledStores, store)
			}
		}
	}

	// Load spreadsheet cloud url if present
	gdocURL := r.FormValue("gdocURL")

	// FormFile returns the first file for the given key `cardListFile`
	// it also returns the FileHeader so we can get the Filename,
	// the Header and the size of the file
	file, handler, err := r.FormFile("cardListFile")
	if err != nil && gdocURL == "" {
		render(w, "upload.html", pageVars)
		return
	} else if err == nil {
		defer file.Close()
	}

	if gdocURL != "" {
		log.Printf("Loading spreadsheet: %+v", gdocURL)

		// Reset the cookie for this preference
		if cachedGdocURL != gdocURL {
			setCookie(w, r, "gdocURL", gdocURL)
			pageVars.RemoteLinkURL = gdocURL
		}
	} else {
		log.Printf("Uploaded File: %+v", handler.Filename)
		log.Printf("File Size: %+v bytes", handler.Size)
		log.Printf("MIME Header: %+v", handler.Header)
	}
	log.Printf("Buylist mode: %+v", blMode)
	log.Printf("Enabled stores: %+v", enabledStores)

	// Save user preferred stores in cookies and make sure the page is updated with those
	if blMode {
		setCookie(w, r, "enabledVendors", strings.Join(enabledStores, "|"))
		pageVars.EnabledVendors = enabledStores
	} else {
		setCookie(w, r, "enabledSellers", strings.Join(enabledStores, "|"))
		pageVars.EnabledSellers = enabledStores
	}

	start := time.Now()

	// Load data
	var uploadedData []UploadEntry
	if gdocURL != "" {
		uploadedData, err = loadSpreadsheet(gdocURL)
	} else if strings.HasSuffix(handler.Filename, ".xls") {
		uploadedData, err = loadOldXls(file)
	} else if strings.HasSuffix(handler.Filename, ".xlsx") {
		uploadedData, err = loadXlsx(file)
	} else {
		uploadedData, err = loadCsv(file, ',')
	}
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
		results = getVendorPrices("", enabledStores, "", cardIds, false, false)
	} else {
		results = getSellerPrices("", enabledStores, "", cardIds, false, false)
	}

	indexKeys := []string{TCG_LOW, TCG_MARKET, TCG_DIRECT_LOW}
	indexResults := getSellerPrices("", indexKeys, "", cardIds, false, false)
	pageVars.IndexEntries = indexResults
	pageVars.IndexKeys = indexKeys

	pageVars.Metadata = map[string]GenericCard{}
	if gdocURL != "" {
		pageVars.SearchQuery = gdocURL
	} else {
		pageVars.SearchQuery = handler.Filename
	}
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
	msg := fmt.Sprintf("%s uploaded %d %s entries from %s, took %v", user, len(cardIds), msgMode, pageVars.SearchQuery, time.Since(start))
	Notify("upload", msg)
	LogPages["Upload"].Println(msg)
	if DevMode {
		log.Println(msg)
	}

	// Touchdown!
	render(w, "upload.html", pageVars)
}

func parseHeader(first []string) (map[string]int, error) {
	if len(first) < 2 {
		return nil, errors.New("too few fields")
	}

	indexMap := map[string]int{}

	// Parse the header to understand where these fields are
	for i, field := range first {
		field = strings.ToLower(field)
		switch {
		case strings.Contains(field, "stock"):
			continue
		case field == "id" || (strings.Contains(field, "id") && (strings.Contains(field, "tcg") || strings.Contains(field, "scyfall"))):
			_, found := indexMap["id"]
			if !found {
				indexMap["id"] = i
			}
		case strings.Contains(field, "name") && !strings.Contains(field, "edition") && !strings.Contains(field, "set"):
			_, found := indexMap["cardName"]
			if !found {
				indexMap["cardName"] = i
			}
		case strings.Contains(field, "edition") || strings.Contains(field, "set"):
			_, found := indexMap["edition"]
			if !found {
				indexMap["edition"] = i
			}
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

	// Set some default values for the mandatory fields
	_, found := indexMap["cardName"]
	if !found {
		indexMap["cardName"] = 0
	}
	_, found = indexMap["edition"]
	if !found {
		indexMap["edition"] = 1
	}

	return indexMap, nil
}

func parseRow(indexMap map[string]int, record []string) UploadEntry {
	var res UploadEntry

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

	var printing string
	_, found = indexMap["printing"]
	if found {
		printing = strings.ToLower(record[indexMap["printing"]])
	}
	if printing == "y" || printing == "yes" || printing == "true" ||
		mtgmatcher.Contains(printing, "foil") ||
		mtgmatcher.Contains(res.Card.Variation, "foil") {
		res.Card.Foil = true
	}

	_, found = indexMap["price"]
	if found {
		res.OriginalPrice, _ = strconv.ParseFloat(record[indexMap["price"]], 64)
	}

	res.CardId, res.MismatchError = mtgmatcher.Match(&res.Card)

	return res
}

func loadSpreadsheet(link string) ([]UploadEntry, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}

	service := spreadsheet.NewServiceWithClient(GoogleDocsClient)

	hash := path.Base(strings.TrimSuffix(u.Path, "/edit"))
	spreadsheet, err := service.FetchSpreadsheet(hash)
	if err != nil {
		return nil, err
	}

	sheetIndex := 0
	for i := 0; i < len(spreadsheet.Sheets); i++ {
		if strings.Contains(strings.ToLower(spreadsheet.Sheets[i].Properties.Title), "mtgban") {
			sheetIndex = i
			break
		}
	}

	sheet, err := spreadsheet.SheetByIndex(uint(sheetIndex))
	if err != nil {
		return nil, err
	}

	if len(sheet.Rows) == 0 {
		return nil, errors.New("empty xls file")
	}

	record := make([]string, len(sheet.Rows[0]))
	for i := range record {
		record[i] = sheet.Rows[0][i].Value
	}

	indexMap, err := parseHeader(record)
	if err != nil {
		return nil, err
	}

	foundHashes := map[string]bool{}
	var i int
	var uploadEntries []UploadEntry
	for {
		i++
		if i > MaxUploadEntries || i > len(sheet.Rows) {
			break
		} else if len(record) != len(sheet.Rows[i]) {
			var res UploadEntry
			res.MismatchError = errors.New("wrong number of fields")
			uploadEntries = append(uploadEntries, res)
			continue
		}

		for j := range record {
			record[j] = sheet.Rows[i][j].Value
		}

		res := parseRow(indexMap, record)

		// Skip repeated entries
		if foundHashes[res.CardId] {
			continue
		}
		if res.MismatchError == nil {
			foundHashes[res.CardId] = true
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

func loadOldXls(reader io.ReadSeeker) ([]UploadEntry, error) {
	f, err := xls.OpenReader(reader, "")
	if err != nil {
		return nil, err
	}

	// Search for the possible main sheet
	sheetIndex := 0
	for i := 0; i < f.NumSheets(); i++ {
		sheet := f.GetSheet(i)
		if sheet != nil && strings.Contains(strings.ToLower(sheet.Name), "mtgban") {
			sheetIndex = i
			break
		}
	}

	sheet := f.GetSheet(sheetIndex)
	if sheet == nil || sheet.MaxRow == 0 {
		return nil, errors.New("empty xls file")
	}

	record := make([]string, sheet.Row(0).LastCol())
	for i := range record {
		record[i] = sheet.Row(0).Col(i)
	}

	indexMap, err := parseHeader(record)
	if err != nil {
		return nil, err
	}

	foundHashes := map[string]bool{}
	var i int
	var uploadEntries []UploadEntry
	for {
		i++
		if i > MaxUploadEntries || i > int(sheet.MaxRow) {
			break
		} else if len(record) != sheet.Row(i).LastCol() {
			var res UploadEntry
			res.MismatchError = errors.New("wrong number of fields")
			uploadEntries = append(uploadEntries, res)
			continue
		}

		for j := range record {
			record[j] = sheet.Row(i).Col(j)
		}

		res := parseRow(indexMap, record)

		// Skip repeated entries
		if foundHashes[res.CardId] {
			continue
		}
		if res.MismatchError == nil {
			foundHashes[res.CardId] = true
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

func loadXlsx(reader io.Reader) ([]UploadEntry, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, err
	}

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("empty xlsx file")
	}

	// Search for the possible main sheet
	sheetIndex := 0
	for i, sheet := range sheets {
		if strings.Contains(strings.ToLower(sheet), "mtgban") {
			sheetIndex = i
			break
		}
	}

	// Get all the rows in the Sheet1.
	rows, err := f.GetRows(sheets[sheetIndex])
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, errors.New("empty sheet")
	}

	indexMap, err := parseHeader(rows[0])
	if err != nil {
		return nil, err
	}

	foundHashes := map[string]bool{}
	var i int
	var uploadEntries []UploadEntry
	for {
		i++
		if i > MaxUploadEntries || i >= len(rows) {
			break
		} else if len(rows[i]) != len(rows[0]) {
			var res UploadEntry
			res.MismatchError = errors.New("wrong number of fields")
			uploadEntries = append(uploadEntries, res)
			continue
		}

		res := parseRow(indexMap, rows[i])

		// Skip repeated entries
		if foundHashes[res.CardId] {
			continue
		}
		if res.MismatchError == nil {
			foundHashes[res.CardId] = true
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

func loadCsv(reader io.ReadSeeker, comma rune) ([]UploadEntry, error) {
	csvReader := csv.NewReader(reader)

	csvReader.TrimLeadingSpace = true
	csvReader.Comma = comma

	// In case we are not using a sane csv
	if comma != ',' {
		csvReader.LazyQuotes = true
	}

	// Load header
	first, err := csvReader.Read()
	if err == io.EOF {
		return nil, errors.New("empty input file")
	}
	if err != nil {
		log.Println("Error reading header:", err)
		return nil, errors.New("error reading file header")
	}

	// If there is a single element, parsing didn't work
	// try again with a different delimiter
	if len(first) == 1 && comma == ',' {
		reader.Seek(0, io.SeekStart)
		return loadCsv(reader, '\t')
	}

	indexMap, err := parseHeader(first)
	if err != nil {
		return nil, err
	}

	foundHashes := map[string]bool{}
	var i int
	var uploadEntries []UploadEntry
	for {
		i++
		if i > MaxUploadEntries {
			break
		}

		record, err := csvReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			var res UploadEntry
			res.MismatchError = err
			uploadEntries = append(uploadEntries, res)
			continue
		}

		res := parseRow(indexMap, record)

		// Skip repeated entries
		if foundHashes[res.CardId] {
			continue
		}

		// Report any errors to the user or track which hash was found
		if res.MismatchError != nil {
			res.MismatchError = fmt.Errorf("record on line %d: %s", i+1, res.MismatchError.Error())
		} else {
			foundHashes[res.CardId] = true
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

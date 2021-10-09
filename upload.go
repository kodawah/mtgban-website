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
	MaxUploadEntries  = 150
	MaxUploadFileSize = 5 << 20

	TooManyEntriesMessage = "Note: you reached the maximum number of entries supported by this tool"
)

type UploadEntry struct {
	Card          mtgmatcher.Card
	CardId        string
	MismatchError error
	OriginalPrice float64

	HasQuantity bool
	Quantity    int
}

func Upload(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Upload", sig)

	// Maximum form size
	r.ParseMultipartForm(MaxUploadFileSize)

	// Check cookies to set preferences
	blMode := readSetFlag(w, r, "mode", "uploadMode")

	// Disable buylist if not permitted
	canBuylist, _ := strconv.ParseBool(GetParamFromSig(sig, "UploadBuylistEnabled"))
	if DevMode && !SigCheck {
		canBuylist = true
	}
	if !canBuylist {
		blMode = false
	}

	// Disable changing stores if not permitted
	canChangeStores, _ := strconv.ParseBool(GetParamFromSig(sig, "UploadChangeStoresEnabled"))
	if DevMode && !SigCheck {
		canChangeStores = true
	}

	// Enable optimizer calculation if allowed for buylists
	optimizerOpt, _ := strconv.ParseBool(GetParamFromSig(sig, "UploadOptimizer"))
	canOptimize := (optimizerOpt || (DevMode && !SigCheck)) && blMode

	// Set flags needed to show elements on the page ui
	pageVars.IsBuylist = blMode
	pageVars.CanBuylist = canBuylist
	pageVars.CanChangeStores = canChangeStores

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
	// Force stores if not allowed to change them
	enabledSellers := readCookie(r, "enabledSellers")
	if len(enabledSellers) == 0 || !canChangeStores {
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
		// Override in case not allowed to change list
		if !canChangeStores {
			stores = Config.AffiliatesList
		}
		for _, store := range stores {
			if SliceStringHas(allSellers, store) {
				enabledStores = append(enabledStores, store)
			}
		}
	}

	// Private call from newspaper
	hashes := r.Form["hashes"]
	if len(hashes) != 0 && len(stores) == 0 {
		if blMode {
			enabledStores = pageVars.EnabledVendors
		} else {
			enabledStores = pageVars.EnabledSellers
		}
	}

	// Load spreadsheet cloud url if present
	gdocURL := r.FormValue("gdocURL")

	// FormFile returns the first file for the given key `cardListFile`
	// it also returns the FileHeader so we can get the Filename,
	// the Header and the size of the file
	file, handler, err := r.FormFile("cardListFile")
	if err != nil && gdocURL == "" && len(hashes) == 0 {
		render(w, "upload.html", pageVars)
		return
	} else if err == nil {
		defer file.Close()
	}

	if len(hashes) != 0 {
		log.Printf("Loading from POST %d cards", len(hashes))
		pageVars.CardHashes = hashes
	} else if gdocURL != "" {
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
	if len(hashes) != 0 {
		uploadedData, err = loadHashes(hashes)
	} else if gdocURL != "" {
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
	if len(hashes) != 0 {
		pageVars.SearchQuery = "hashes"
	} else if gdocURL != "" {
		pageVars.SearchQuery = gdocURL
	} else {
		pageVars.SearchQuery = handler.Filename
	}
	pageVars.ScraperKeys = enabledStores
	pageVars.CompactEntries = results
	pageVars.UploadEntries = uploadedData
	pageVars.TotalEntries = map[string]float64{}

	var optimizedResults map[string][]string
	var optimizedTotals map[string]float64
	var highestTotal float64

	if canOptimize {
		optimizedResults = map[string][]string{}
		optimizedTotals = map[string]float64{}
	}

	missingCounts := map[string]int{}
	missingPrices := map[string]float64{}

	for i := range uploadedData {
		var bestPrice float64
		var bestStore string

		cardId := uploadedData[i].CardId

		// Search for any missing entries (ie cards not sold or bought by a vendor)
		for _, shorthand := range enabledStores {
			_, found := results[cardId][shorthand]
			if !found {
				missingCounts[shorthand]++
				missingPrices[shorthand] += getPrice(indexResults[cardId][TCG_LOW])
			}
		}

		// Summary of the entries
		pageVars.TotalEntries[TCG_LOW] += getPrice(indexResults[cardId][TCG_LOW])

		// Run summaries for each vendor
		for shorthand, banPrice := range results[cardId] {
			price := getPrice(banPrice)
			// Skip empty results
			if price == 0 {
				continue
			}

			// Adjust for quantity
			if uploadedData[i].HasQuantity {
				price *= float64(uploadedData[i].Quantity)
				pageVars.TotalQuantity += uploadedData[i].Quantity
			}

			// Add to totals
			pageVars.TotalEntries[shorthand] += price

			if !canOptimize {
				continue
			}

			if blMode {
				if bestPrice < price {
					bestPrice = price
					bestStore = shorthand
				}
			} else {
				if bestPrice == 0 || bestPrice > price {
					bestPrice = price
					bestStore = shorthand
				}
			}
		}

		if canOptimize && bestPrice != 0 {
			optimizedResults[bestStore] = append(optimizedResults[bestStore], uploadedData[i].CardId)
			optimizedTotals[bestStore] += bestPrice
			highestTotal += bestPrice
		}
	}
	if canOptimize {
		pageVars.Optimized = optimizedResults
		pageVars.OptimizedTotals = optimizedTotals
		pageVars.HighestTotal = highestTotal
	}

	pageVars.MissingCounts = missingCounts
	pageVars.MissingPrices = missingPrices

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

func getPrice(banPrice *BanPrice) float64 {
	if banPrice == nil {
		return 0
	}

	// Grab the correct Price
	price := banPrice.Regular
	if price == 0 {
		price = banPrice.Foil
		if price == 0 {
			price = banPrice.Etched
		}
	}

	return price
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
		case strings.Contains(field, "foil") || strings.Contains(field, "printing") || strings.Contains(field, "finish") || field == "f/nf" || field == "nf/f":
			_, found := indexMap["printing"]
			if !found {
				indexMap["printing"] = i
			}
		case strings.Contains(field, "sku"):
			_, found := indexMap["sku"]
			if !found {
				indexMap["sku"] = i
			}
		case strings.Contains(field, "condition"):
			_, found := indexMap["conditions"]
			if !found {
				indexMap["conditions"] = i
			}
		case strings.Contains(field, "price") || strings.Contains(field, "low"):
			_, found := indexMap["price"]
			if !found {
				indexMap["price"] = i
			}
		case (strings.Contains(field, "quantity") || strings.Contains(field, "qty") || strings.Contains(field, "stock") || strings.Contains(field, "count") || strings.Contains(field, "have")) && !strings.HasPrefix(field, "add") && !strings.HasPrefix(field, "set"):
			_, found := indexMap["quantity"]
			if !found {
				indexMap["quantity"] = i
			}
		case strings.Contains(field, "title") && !strings.Contains(field, "variant"):
			_, found := indexMap["title"]
			if !found {
				indexMap["title"] = i
			}
		}
	}

	// Set some default values for the mandatory fields
	_, found := indexMap["cardName"]
	if !found {
		indexMap["cardName"] = 0
		// Used by some formats that do not set a card name
		i, found := indexMap["title"]
		if found {
			indexMap["cardName"] = i
		}
	}
	_, found = indexMap["edition"]
	if !found {
		indexMap["edition"] = 1
	}

	log.Println("Header map:", indexMap)
	return indexMap, nil
}

func parseRow(indexMap map[string]int, record []string, foundHashes map[string]bool) (UploadEntry, error) {
	var res UploadEntry
	var found bool

	// Skip empty lines
	hasContent := false
	for _, field := range record {
		if field != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return res, errors.New("empty line")
	}

	// Load quantity, and skip it if it's present and zero
	_, found = indexMap["quantity"]
	if found {
		qty := record[indexMap["quantity"]]
		num, err := strconv.Atoi(qty)
		if err == nil {
			res.HasQuantity = true
			res.Quantity = num
		}
	}
	if res.HasQuantity && res.Quantity == 0 {
		return res, errors.New("no stock")
	}

	_, found = indexMap["id"]
	if found {
		res.Card.Id = record[indexMap["id"]]
	}

	res.Card.Name = record[indexMap["cardName"]]
	res.Card.Edition = record[indexMap["edition"]]

	_, found = indexMap["variant"]
	if found {
		res.Card.Variation = record[indexMap["variant"]]
	}

	var sku string
	_, found = indexMap["sku"]
	if found {
		sku = strings.ToLower(record[indexMap["sku"]])
	}
	var conditions string
	_, found = indexMap["conditions"]
	if found {
		conditions = strings.ToLower(record[indexMap["conditions"]])
	}
	var printing string
	_, found = indexMap["printing"]
	if found {
		printing = strings.ToLower(record[indexMap["printing"]])
	}
	switch printing {
	case "y", "yes", "true", "t", "1":
		res.Card.Foil = true
	default:
		if strings.Contains(printing, "foil") ||
			strings.Contains(conditions, "foil") ||
			strings.Contains(strings.ToLower(res.Card.Variation), "foil") ||
			strings.Contains(sku, "-f-") || strings.Contains(sku, "-fo-") {
			res.Card.Foil = true
		}
	}

	_, found = indexMap["price"]
	if found {
		res.OriginalPrice, _ = mtgmatcher.ParsePrice(record[indexMap["price"]])
	}

	res.CardId, res.MismatchError = mtgmatcher.Match(&res.Card)

	if foundHashes[res.CardId] {
		return res, errors.New("repeated")
	}
	if res.MismatchError == nil {
		foundHashes[res.CardId] = true
	}

	return res, nil
}

func loadHashes(hashes []string) ([]UploadEntry, error) {
	var uploadEntries []UploadEntry

	for i := range hashes {
		uploadEntries = append(uploadEntries, UploadEntry{
			CardId: hashes[i],
		})
	}

	return uploadEntries, nil
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
		if i > MaxUploadEntries || i >= len(sheet.Rows) {
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

		res, err := parseRow(indexMap, record, foundHashes)
		if err != nil {
			continue
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
		if i > MaxUploadEntries || i >= int(sheet.MaxRow) {
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

		res, err := parseRow(indexMap, record, foundHashes)
		if err != nil {
			continue
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

		res, err := parseRow(indexMap, rows[i], foundHashes)
		if err != nil {
			continue
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
		_, err = reader.Seek(0, io.SeekStart)
		if err != nil {
			return nil, err
		}
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

		res, err := parseRow(indexMap, record, foundHashes)
		if err != nil {
			continue
		}

		// Tweak the message to the format from csv errors
		if res.MismatchError != nil {
			res.MismatchError = fmt.Errorf("record on line %d: %s", i+1, res.MismatchError.Error())
		}

		uploadEntries = append(uploadEntries, res)
	}

	return uploadEntries, nil
}

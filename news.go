package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

const (
	newsPageSize = 25
)

type Heading struct {
	// The header string
	Title string
	// The field can be sorted
	CanSort bool
	// The name of the field to be sorted
	Field string
	// Need dolla sign prepended
	IsDollar bool
	// This is a percentage
	IsPerc bool
	// Do not display this field in HTML
	IsHidden bool
	// This field can be sorted when filtered
	ConditionalSort bool
}

type NewspaperPage struct {
	// Title of the page
	Title string
	// Short description of the current page
	Desc string
	// Name of the page used in the query parameter
	Option string
	// The query run to obtain data
	Query string
	// Default orting option
	Sort string
	// The name of the columns and their properties
	Head []Heading
	// Whether this table has lots of fields that need wider display
	Large bool
	// How many elements are present before the card triplet
	Offset int
	// Which field to use for price comparison
	Priced string
	// Which field to use for percentage change comparison
	PercChanged string
}

var NewspaperPages = []NewspaperPage{
	NewspaperPage{
		Title:  "Top 25 Singles (3 Week Market Review)",
		Desc:   "Rankings are weighted via prior 21, 15, and 7 days via Retail, Buylist, and several other criteria to arrive at an overall ranking",
		Offset: 3,
		Priced: "n.Buylist",
		Option: "review",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       n.Ranking,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Retail, n.Buylist, n.Vendors
                FROM top_25 n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.uuid <> ""`,
		Sort: "Ranking",
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Ranking",
				CanSort: true,
				Field:   "Ranking",
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:    "Retail",
				CanSort:  true,
				Field:    "Retail",
				IsDollar: true,
			},
			Heading{
				Title:    "Buylist",
				CanSort:  true,
				Field:    "Buylist",
				IsDollar: true,
			},
			Heading{
				Title:   "Vendors",
				CanSort: true,
				Field:   "Vendors",
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Decrease in Vendor Listings",
		Desc:   "Information Sourced from TCG: Stock decreases indicate that there is not enough supply to meet current demand across the reviewed time period (tl:dr - Seek these out)",
		Offset: 2,
		Option: "stock_dec",

		PercChanged: "n.Week_Ago_Sellers_Chg",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Todays_Sellers, n.Week_Ago_Sellers, n.Month_Ago_Sellers, n.Week_Ago_Sellers_Chg,
                       CASE
                           WHEN n.Week_Ago_Sellers < n.Month_Ago_Sellers
                           THEN CASE
                               WHEN n.Todays_Sellers <= n.Week_Ago_Sellers / 3     THEN 'S'
                               WHEN n.Todays_Sellers <= n.Week_Ago_Sellers / 2     THEN 'A'
                               WHEN n.Todays_Sellers <= n.Week_Ago_Sellers * 2 / 3 THEN 'B'
                               WHEN n.Todays_Sellers <= n.Week_Ago_Sellers * 3 / 4 THEN 'C'
                               WHEN n.Todays_Sellers <= n.Week_Ago_Sellers * 4 / 5 THEN 'D'
                               WHEN n.Todays_Sellers <  n.Week_Ago_Sellers         THEN 'E'
                               ELSE ''
                           END
                           ELSE ''
                       END AS 'Trending'
                FROM vendor_levels n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.Week_Ago_Sellers_Chg is not NULL and n.Week_Ago_Sellers_Chg != 0`,
		Sort: "n.Week_Ago_Sellers_Chg DESC",
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Today's Sellers",
				CanSort: true,
				Field:   "Todays_Sellers",
			},
			Heading{
				Title:   "Last Week Sellers",
				CanSort: true,
				Field:   "Week_Ago_Sellers",
			},
			Heading{
				Title:   "Month Ago Sellers",
				CanSort: true,
				Field:   "Month_Ago_Sellers",
			},
			Heading{
				Title:   "Weekly % Change",
				CanSort: true,
				Field:   "Week_Ago_Sellers_Chg",
				IsPerc:  true,
			},
			Heading{
				Title:   "Tier",
				CanSort: true,
				Field:   "Trending",
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Increase in Vendor Listings",
		Desc:   "Information Sourced from TCG: Stock Increases indicate that there is more than enough supply to meet current demand across the reviewed time period (tl:dr - Avoid These)",
		Offset: 2,
		Option: "stock_inc",

		PercChanged: "n.Week_Ago_Sellers_Chg",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Todays_Sellers, n.Week_Ago_Sellers, n.Month_Ago_Sellers, n.Week_Ago_Sellers_Chg
                FROM vendor_levels n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.Week_Ago_Sellers_Chg is not NULL and n.Week_Ago_Sellers_Chg != 0 AND a.rdate <= CURRENT_DATE()`,
		Sort: "n.Week_Ago_Sellers_Chg ASC",
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Today's Seller",
				CanSort: true,
				Field:   "Todays_Sellers",
			},
			Heading{
				Title:   "Last Week",
				CanSort: true,
				Field:   "Week_Ago_Sellers",
			},
			Heading{
				Title:   "Month Ago",
				CanSort: true,
				Field:   "Month_Ago_Sellers",
			},
			Heading{
				Title:   "Weekly % Change",
				CanSort: true,
				Field:   "Week_Ago_Sellers_Chg",
				IsPerc:  true,
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Increase in Buylist Offer",
		Desc:   "Information Sourced from CK: buylist increases indicate a higher sales rate (eg. higher demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Offset: 2,
		Priced: "n.Todays_BL",
		Option: "buylist_inc",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Todays_BL, n.Yesterday_BL, n.Week_Ago_BL, n.Month_Ago_BL, n.Week_Ago_BL_Chg
                FROM buylist_levels n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.Week_Ago_BL_Chg is not NULL and n.Week_Ago_BL_Chg != 0 and n.Yesterday_BL >= 1.25 and n.Todays_BL >= 1.25`,
		Sort: "n.Week_Ago_BL_Chg DESC",
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:    "Today's Buylist",
				CanSort:  true,
				Field:    "Todays_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Yesterday",
				CanSort:  true,
				Field:    "Yesterday_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Last Week",
				CanSort:  true,
				Field:    "Week_Ago_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Last Month",
				CanSort:  true,
				Field:    "Month_Ago_BL",
				IsDollar: true,
			},
			Heading{
				Title:   "Weekly % Change",
				CanSort: true,
				Field:   "Week_Ago_BL_Chg",
				IsPerc:  true,
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Decrease in Buylist Offer",
		Desc:   "Information Sourced from CK: Buylist Decreases indicate a declining sales rate (eg, Less demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Offset: 2,
		Priced: "n.Todays_BL",
		Option: "buylist_dec",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Todays_BL, n.Yesterday_BL, n.Week_Ago_BL, n.Month_Ago_BL, n.Week_Ago_BL_Chg
                FROM buylist_levels n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.Week_Ago_BL_Chg is not NULL and n.Week_Ago_BL_Chg != 0 and n.Yesterday_BL >= 1.25 and n.Todays_BL >= 1.25`,
		Sort: "n.Week_Ago_BL_Chg ASC",
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:    "Today's Buylist",
				CanSort:  true,
				Field:    "Todays_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Yesterday",
				CanSort:  true,
				Field:    "Yesterday_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Last Week",
				CanSort:  true,
				Field:    "Week_Ago_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Last Month",
				CanSort:  true,
				Field:    "Month_Ago_BL",
				IsDollar: true,
			},
			Heading{
				Title:   "Weekly % Change",
				CanSort: true,
				Field:   "Week_Ago_BL_Chg",
				IsPerc:  true,
			},
		},
	},
	NewspaperPage{
		Title:  "Buylist Growth Forecast",
		Desc:   "Forecasting Card Kingdom's Buylist Offers on Cards",
		Offset: 2,
		Priced: "n.Recent_BL",
		Option: "ensemble_forecast",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number, a.Rarity,
                       n.Recent_BL, n.Historical_plus_minus, n.Historical_Median, n.Historical_Max, n.Forecasted_BL, n.Forecast_plus_minus, n.Target_Date, n.Tier, n.Behavior, n.custom_sort
                FROM ensemble_forecast n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid
                WHERE n.uuid <> ""`,
		Sort:  "n.custom_sort",
		Large: true,
		Head: []Heading{
			Heading{
				IsHidden: true,
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:   "Card Name",
				CanSort: true,
				Field:   "Name",
			},
			Heading{
				Title:   "Edition",
				CanSort: true,
				Field:   "a.Set",
			},
			Heading{
				Title:           "#",
				ConditionalSort: true,
				Field:           "a.Number",
			},
			Heading{
				IsHidden: true,
			},
			Heading{
				Title:    "Most Recent BL",
				CanSort:  true,
				Field:    "Recent_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Historical +/-",
				CanSort:  true,
				Field:    "Historical_plus_minus",
				IsDollar: true,
			},
			Heading{
				Title:    "Historical Median",
				CanSort:  true,
				Field:    "Historical_Median",
				IsDollar: true,
			},
			Heading{
				Title:    "Historical Max",
				CanSort:  true,
				Field:    "Historical_Max",
				IsDollar: true,
			},
			Heading{
				Title:    "Forecasted BL",
				CanSort:  true,
				Field:    "Forecasted_BL",
				IsDollar: true,
			},
			Heading{
				Title:    "Forecast +/-",
				CanSort:  true,
				Field:    "Forecast_plus_minus",
				IsDollar: true,
			},
			Heading{
				Title:   "Forecasted Date",
				CanSort: true,
				Field:   "Target_Date",
			},
			Heading{
				Title:   "Tier",
				CanSort: true,
				Field:   "Tier",
			},
			Heading{
				Title:   "Behavior",
				CanSort: true,
				Field:   "Behavior",
			},
			Heading{
				IsHidden: true,
			},
		},
	},
	NewspaperPage{
		Title:  "Newspaper Settings",
		Option: "options",
	},
}

var NewspaperAllRarities = []string{"", "M", "R", "U", "C", "S"}

func Newspaper(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Newspaper", sig)

	var db *sql.DB
	enabled := GetParamFromSig(sig, "NewsEnabled")
	if enabled == "1day" {
		db = Newspaper1dayDB
		pageVars.IsOneDay = true
	} else if enabled == "3day" {
		db = Newspaper3dayDB
	} else if enabled == "0day" || (DevMode && !SigCheck) {
		force3day := readSetFlag(w, r, "force3day", "MTGBANNewpaperPref")
		if force3day {
			db = Newspaper3dayDB
		} else {
			db = Newspaper1dayDB
			pageVars.IsOneDay = true
		}
		pageVars.CanSwitchDay = true
	} else {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgDenied

		render(w, "news.html", pageVars)
		return
	}

	pageVars.ToC = NewspaperPages

	r.ParseForm()
	page := r.FormValue("page")
	sort := r.FormValue("sort")
	dir := r.FormValue("dir")
	filter := r.FormValue("filter")
	rarity := r.FormValue("rarity")
	minPrice, _ := strconv.ParseFloat(r.FormValue("min_price"), 64)
	maxPrice, _ := strconv.ParseFloat(r.FormValue("max_price"), 64)
	minPercChange, _ := strconv.ParseFloat(r.FormValue("min_change"), 64)
	maxPercChange, _ := strconv.ParseFloat(r.FormValue("max_change"), 64)
	pageIndexStr := r.FormValue("index")
	pageIndex, _ := strconv.Atoi(pageIndexStr)
	var query, defSort string
	var pages int

	pageVars.Nav = insertNavBar("Newspaper", pageVars.Nav, []NavElem{
		NavElem{
			Name:   "Lairs",
			Short:  "🤫",
			Link:   "/newspaper?page=secret",
			Active: page == "secret",
			Class:  "selected",
		},
	})

	switch page {
	default:
		pageVars.Title = "Index"

		render(w, "news.html", pageVars)

		return
	case "options":
		pageVars.Title = "Options"

		pageVars.Editions = AllEditionsKeys
		pageVars.EditionsMap = AllEditionsMap

		render(w, "news.html", pageVars)

		return
	case "secret":
		pageVars.Title = "Secret Lair Data"

		pageVars.IsSealed = true

		w.Header().Add("Content-Security-Policy", "frame-ancestors 'self' https://datastudio.google.com;")

		render(w, "news.html", pageVars)

		return
	}

	pageVars.SortOption = sort
	pageVars.SortDir = dir
	pageVars.FilterSet = filter
	pageVars.FilterRarity = rarity
	pageVars.FilterMinPrice = minPrice
	pageVars.FilterMaxPrice = maxPrice
	pageVars.FilterMinPercChange = minPercChange
	pageVars.FilterMaxPercChange = maxPercChange
	pageVars.Rarities = NewspaperAllRarities

	var skipEditions string
	skipEditionsOpt := readCookie(r, "NewspaperList")
	if skipEditionsOpt != "" {
		sets := mtgmatcher.GetSets()
		filters := strings.Split(skipEditionsOpt, ",")
		for _, code := range filters {
			// XXX: is set code available on the db row?
			set, found := sets[code]
			if !found {
				continue
			}
			skipEditions += " AND a.Set <> \"" + set.Name + "\""
		}
	}

	for _, newspage := range NewspaperPages {
		if newspage.Option == page {
			pageVars.Page = newspage.Option
			pageVars.Title = newspage.Title
			pageVars.InfoMessage = newspage.Desc
			pageVars.Headings = newspage.Head
			pageVars.LargeTable = newspage.Large
			pageVars.OffsetCards = newspage.Offset

			// Get the total number of rows for the query
			qs := strings.Split(newspage.Query, "FROM")
			if len(qs) != 2 {
				pageVars.Title = "Errors have been made"
				pageVars.ErrorMessage = ErrMsgDenied

				render(w, "news.html", pageVars)
				return
			}

			// Set query to retrieve total number of matches
			subQuery := "SELECT COUNT(DISTINCT n.uuid) FROM" + qs[1]

			// Add any extra filter that might affect number of results
			if filter != "" {
				subQuery += " AND a.Set = \"" + filter + "\""
			}
			if rarity != "" {
				subQuery += " AND a.Rarity = \"" + rarity + "\""
			}
			if newspage.Priced != "" && minPrice != 0 {
				subQuery += " AND " + newspage.Priced + " > " + fmt.Sprintf("%.2f", minPrice)
			}
			if newspage.Priced != "" && maxPrice != 0 {
				subQuery += " AND " + newspage.Priced + " < " + fmt.Sprintf("%.2f", maxPrice)
			}
			if newspage.PercChanged != "" && minPercChange != 0 {
				subQuery += " AND " + newspage.PercChanged + " > " + fmt.Sprintf("%.2f", minPercChange/100)
			}
			if newspage.PercChanged != "" && maxPercChange != 0 {
				subQuery += " AND " + newspage.PercChanged + " < " + fmt.Sprintf("%.2f", maxPercChange/100)
			}

			subQuery += skipEditions

			if newspage.Priced != "" {
				pageVars.CanFilterByPrice = true
			}
			if newspage.PercChanged != "" {
				pageVars.CanFilterByPercentage = true
			}

			// Sub Go!
			err := db.QueryRow(subQuery).Scan(&pages)
			if err != nil {
				log.Println("pages disabled", err)
			}
			// This integer division is equivalent to math.Floor()
			pages /= newsPageSize

			pageVars.TotalIndex = pages
			if pageIndex >= 0 && pageIndex <= pages {
				pageVars.CurrentIndex = pageIndex
			} else {
				// Reset the index if we over or underflow
				pageIndex = 0
			}
			if pageVars.CurrentIndex > 0 {
				pageVars.PrevIndex = pageVars.CurrentIndex - 1
			}
			if pageVars.CurrentIndex < pages {
				pageVars.NextIndex = pageVars.CurrentIndex + 1
			}

			query = newspage.Query
			defSort = newspage.Sort

			// Repeat as above to retrieve the possible editions
			subQuery = "SELECT DISTINCT a.Set FROM" + qs[1] + skipEditions + " ORDER BY a.Set ASC"
			rows, err := db.Query(subQuery)
			if err != nil {
				log.Println("editions disabled", err)
				break
			}
			// First element is always initialized
			pageVars.Editions = []string{""}
			// Iterate over subresults
			for rows.Next() {
				var tmp string
				err := rows.Scan(&tmp)
				if err != nil {
					continue
				}
				pageVars.Editions = append(pageVars.Editions, tmp)
			}

			break
		}
	}

	// Add any extra filter before sorting
	// Note that this requires every query to end with an applicable WHERE clause
	if filter != "" {
		query += " AND a.Set = \"" + filter + "\""
	}
	if rarity != "" {
		query += " AND a.Rarity = \"" + rarity + "\""
	}

	// Check for price limits
	if minPrice != 0 || maxPrice != 0 {
		for _, newspage := range NewspaperPages {
			if newspage.Option == page && newspage.Priced != "" {
				if minPrice != 0 {
					query += " AND " + newspage.Priced + " > " + fmt.Sprintf("%.2f", minPrice)
				}
				if maxPrice != 0 {
					query += " AND " + newspage.Priced + " < " + fmt.Sprintf("%.2f", maxPrice)
				}
			}
		}
	}

	if minPercChange != 0 || maxPercChange != 0 {
		for _, newspage := range NewspaperPages {
			if newspage.Option == page && newspage.PercChanged != "" {
				if minPercChange != 0 {
					query += " AND " + newspage.PercChanged + " > " + fmt.Sprintf("%.2f", minPercChange/100)
				}
				if maxPercChange != 0 {
					query += " AND " + newspage.PercChanged + " < " + fmt.Sprintf("%.2f", maxPercChange/100)
				}
			}
		}
	}

	query += skipEditions

	// Set sorting options
	if sort != "" {
		// Make sure this field is allowed to be sorted
		canSort := false
		for i := range pageVars.Headings {
			if pageVars.Headings[i].Field == sort {
				canSort = pageVars.Headings[i].CanSort
				if pageVars.Headings[i].ConditionalSort && filter != "" {
					canSort = true
				}
				break
			}
		}
		if canSort {
			// Define a custom order for our special scale
			if sort == "Trending" {
				sort = `CASE
                            WHEN Trending = 'S' THEN '7'
                            WHEN Trending = 'A' THEN '6'
                            WHEN Trending = 'B' THEN '5'
                            WHEN Trending = 'C' THEN '4'
                            WHEN Trending = 'D' THEN '3'
                            WHEN Trending = 'E' THEN '2'
                            WHEN Trending = ''  THEN '1'
                            ELSE Trending
                        END`
			}
			query += " ORDER BY " + sort
			if dir == "asc" {
				query += " ASC"
			} else if dir == "desc" {
				query += " DESC"
			}
		}
	} else if defSort != "" {
		query += " ORDER BY " + defSort
	}
	// Keep things limited + pagination
	query = fmt.Sprintf("%s LIMIT %d OFFSET %d", query, newsPageSize, newsPageSize*pageIndex)

	// GO GO GO
	rows, err := db.Query(query)
	if err != nil {
		log.Println(query, err)
		return
	}

	// Retrieve columns to know how many fields to read
	cols, err := rows.Columns()
	if err != nil {
		log.Println("Failed to get columns", err)
		return
	}

	// Result is your slice string
	rawResult := make([][]byte, len(cols))
	result := make([]string, len(cols))

	// A temporary interface{} slice, containing a variable number of fields
	dest := make([]interface{}, len(cols))
	for i := range rawResult {
		// Put pointers to each string in the interface slice
		dest[i] = &rawResult[i]
	}

	// Allocate the main table scheleton
	pageVars.Table = make([][]string, newsPageSize)

	i := 0
	for rows.Next() {
		err := rows.Scan(dest...)
		if err != nil {
			log.Println(err)
			continue
		}

		// Convert the parsed fields into usable strings
		for j, raw := range rawResult {
			if raw == nil {
				result[j] = "n/a"
			} else {
				result[j] = string(raw)
			}
		}

		if len(result) < 2 {
			log.Println("empty row")
			continue
		}

		// Will iterate over card data in the template, since it's limited
		// to the results actually available
		pageVars.Cards = append(pageVars.Cards, uuid2card(result[1], true))
		pageVars.CardHashes = append(pageVars.CardHashes, result[1])

		// Allocate a table row with as many fields as returned by the SELECT
		pageVars.Table[i] = make([]string, len(result))
		for j := range pageVars.Table[i] {
			pageVars.Table[i][j] = result[j]
		}

		// Next row!
		i++
	}

	for _, c := range pageVars.Cards {
		if c.Reserved {
			pageVars.HasReserved = true
		}
		if c.Stocks {
			pageVars.HasStocks = true
		}
		if c.SypList {
			pageVars.HasSypList = true
		}
	}

	if len(pageVars.Cards) == 0 {
		if filter == "" && rarity == "" {
			pageVars.InfoMessage = "Newspaper is on strike (notify devs!)"
		} else {
			pageVars.InfoMessage = "No results for the current filter options"
		}
	}

	render(w, "news.html", pageVars)
}

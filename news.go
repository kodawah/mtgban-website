package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

const (
	newsPageSize = 25
)

type Heading struct {
	Title    string
	CanSort  bool
	Field    string
	IsDollar bool
	IsPerc   bool
	IsHidden bool
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
}

var NewspaperPages = []NewspaperPage{
	NewspaperPage{
		Title:  "Top 25 Singles (3 Week Market Review)",
		Desc:   "Rankings are weighted via prior 21, 15, and 7 days via Retail, Buylist, and several other criteria to arrive at an overall ranking",
		Offset: 3,
		Option: "review",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       n.Ranking,
                       a.Name, a.Set, a.Number,
                       n.Retail, n.Buylist, n.Vendors
                FROM top_25 n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid`,
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
				Title: "#",
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
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
                       n.Todays_Sellers, n.Week_Ago_Sellers, n.Month_Ago_Sellers, n.Week_Ago_Sellers_Chg
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
				Title: "#",
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
		},
	},
	NewspaperPage{
		Title:  "Greatest Increase in Vendor Listings",
		Desc:   "Information Sourced from TCG: Stock Increases indicate that there is more than enough supply to meet current demand across the reviewed time period (tl:dr - Avoid These)",
		Offset: 2,
		Option: "stock_inc",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
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
				Title: "#",
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
		Option: "buylist_inc",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
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
				Title: "#",
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
		Option: "buylist_dec",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
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
				Title: "#",
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
		Option: "ensemble_forecast",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
                       n.Recent_BL, n.Historical_plus_minus, n.Historical_Median, n.Historical_Max, n.Forecasted_BL, n.Forecast_plus_minus, n.Target_Date, n.Tier, n.Behavior, n.custom_sort
                FROM ensemble_forecast n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid`
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
				Title: "#",
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
		Title:  "Forecast Performance",
		Desc:   "Reviewing historical forecasts made with the current day",
		Offset: 2,
		Option: "ensemble_performance",
		Query: `SELECT DISTINCT n.row_names, n.uuid,
                       a.Name, a.Set, a.Number,
                       n.original_bl, n.max_forecast_value, n.current_val, n.classification, n.accuracy_metric, n.custom_sort
                FROM ensemble_performance n
                LEFT JOIN mtgjson_portable a ON n.uuid = a.uuid`
		Sort: "n.custom_sort",
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
				Title: "#",
			},
			Heading{
				Title:    "Original BL",
				CanSort:  true,
				Field:    "original_bl",
				IsDollar: true,
			},
			Heading{
				Title:    "Forecasted Value",
				CanSort:  true,
				Field:    "max_forecast_value",
				IsDollar: true,
			},
			Heading{
				Title:    "Current BL",
				CanSort:  true,
				Field:    "current_val",
				IsDollar: true,
			},
			Heading{
				Title:   "Classification",
				CanSort: true,
				Field:   "classification",
			},
			Heading{
				Title:   "Accuracy",
				CanSort: true,
				Field:   "accuracy_metric",
				IsPerc:  true,
			},
			Heading{
				IsHidden: true,
			},
		},
	},
}

func Newspaper(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Newspaper", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = ErrMsgRestart

		render(w, "news.html", pageVars)
		return
	}

	arbitParam, _ := GetParamFromSig(sig, "Newspaper")
	canSearch, _ := strconv.ParseBool(arbitParam)
	if SigCheck && !canSearch {
		pageVars.Title = "This feature is BANned"
		pageVars.ErrorMessage = ErrMsgPlus
		pageVars.ShowPromo = true

		render(w, "news.html", pageVars)
		return
	}

	var db *sql.DB
	enabled, _ := GetParamFromSig(sig, "NewsEnabled")
	if enabled == "1day" {
		db = Newspaper1dayDB
		pageVars.IsOneDay = true
	} else if enabled == "3day" {
		db = Newspaper3dayDB
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
	pageIndexStr := r.FormValue("index")
	pageIndex, _ := strconv.Atoi(pageIndexStr)
	var query, defSort string
	var pages int

	if page == "" {
		pageVars.Title = "Index"

		render(w, "news.html", pageVars)

		return
	}

	pageVars.SortOption = sort
	pageVars.SortDir = dir

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
			err := db.QueryRow("SELECT COUNT(DISTINCT n.uuid) FROM" + qs[1]).Scan(&pages)
			if err != nil {
				log.Println("pages disabled", err)
			}
			// This integer division is equivalent to math.Floor()
			pages /= newsPageSize

			pageVars.TotalIndex = pages
			if pageIndex >= 0 && pageIndex <= pages {
				pageVars.CurrentIndex = pageIndex
			}
			if pageVars.CurrentIndex > 0 {
				pageVars.PrevIndex = pageVars.CurrentIndex - 1
			}
			if pageVars.CurrentIndex < pages {
				pageVars.NextIndex = pageVars.CurrentIndex + 1
			}

			query = newspage.Query
			defSort = newspage.Sort
			break
		}
	}

	// Set sorting options
	if sort != "" {
		// Make sure this field is allowed to be sorted
		canSort := false
		for _, head := range pageVars.Headings {
			if head.Field == sort {
				canSort = head.CanSort
				break
			}
		}
		if canSort {
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
			break
		}
		if c.Stocks {
			pageVars.HasStocks = true
		}
	}

	if len(pageVars.Cards) == 0 {
		pageVars.InfoMessage = "Newspaper is on strike"
	}

	render(w, "news.html", pageVars)
}

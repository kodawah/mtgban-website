package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/kodabb/go-mtgmatcher/mtgmatcher"
)

const (
	newsPageSize = 25
)

type GenericCard struct {
	Name     string
	Edition  string
	SetCode  string
	Number   string
	Keyrune  string
	ImageURL string
	Reserved bool
}

type Heading struct {
	Title    string
	CanSort  bool
	Field    string
	IsDollar bool
	IsPerc   bool
}

type NewspaperPage struct {
	Title  string
	Desc   string
	Option string
	Query  string
	Sort   string
	Head   []Heading
}

var NewspaperPages = []NewspaperPage{
	NewspaperPage{
		Title:  "Top 25 Singles (3 Week Market Review)",
		Desc:   "Rankings are weighted via prior 21, 15, and 7 days via Retail, Buylist, and several other criteria to arrive at an overall ranking",
		Option: "review",
		Query:  "SELECT * FROM top_25",
		Sort:   "Ranking",
		Head: []Heading{
			Heading{
				Title:   "Ranking",
				CanSort: true,
				Field:   "Ranking",
			},
			Heading{
				Title: "Card Name",
			},
			Heading{
				Title: "Edition",
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
		Option: "stock_dec",
		Query: `SELECT n.row_names, n.uuid, n.Todays_Sellers, n.Week_Ago_Sellers, n.Month_Ago_Sellers, n.Week_Ago_Sellers_Chg
                FROM vendor_levels n
                WHERE n.Week_Ago_Sellers_Chg is not NULL and n.Week_Ago_Sellers_Chg != 0`,
		Sort: "n.Week_Ago_Sellers_Chg DESC",
		Head: []Heading{
			Heading{
				Title: "Card Name",
			},
			Heading{
				Title: "Edition",
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
		Option: "stock_inc",
		Query: `SELECT n.row_names, n.uuid, n.Todays_Sellers, n.Week_Ago_Sellers, n.Month_Ago_Sellers, n.Week_Ago_Sellers_Chg
                FROM vendor_levels n
                WHERE n.Week_Ago_Sellers_Chg is not NULL and n.Week_Ago_Sellers_Chg != 0`,
		Sort: "n.Week_Ago_Sellers_Chg ASC",
		Head: []Heading{
			Heading{
				Title: "Card Name",
			},
			Heading{
				Title: "Edition",
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
		Option: "buylist_inc",
		Query: `SELECT n.row_names, n.uuid, n.Todays_BL, n.Yesterday_BL, n.Week_Ago_BL, n.Month_Ago_BL, n.Week_Ago_BL_Chg
                FROM buylist_levels n
                WHERE n.Week_Ago_BL_Chg is not NULL and n.Week_Ago_BL_Chg != 0 and n.Yesterday_BL >= 1.25 and n.Todays_BL >= 1.25`,
		Sort: "n.Week_Ago_BL_Chg DESC",
		Head: []Heading{
			Heading{
				Title: "Card Name",
			},
			Heading{
				Title: "Edition",
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
				Field:   "Week_Ago_Sellers_Chg",
				IsPerc:  true,
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Decrease in Buylist Offer",
		Desc:   "Information Sourced from CK: Buylist Decreases indicate a declining sales rate (eg, Less demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Option: "buylist_dec",
		Query: `SELECT n.row_names, n.uuid, n.Todays_BL, n.Yesterday_BL, n.Week_Ago_BL, n.Month_Ago_BL, n.Week_Ago_BL_Chg
                FROM buylist_levels n
                WHERE n.Week_Ago_BL_Chg is not NULL and n.Week_Ago_BL_Chg != 0 and n.Yesterday_BL >= 1.25 and n.Todays_BL >= 1.25`,
		Sort: "n.Week_Ago_BL_Chg ASC",
		Head: []Heading{
			Heading{
				Title: "Card Name",
			},
			Heading{
				Title: "Edition",
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
				Field:   "Week_Ago_Sellers_Chg",
				IsPerc:  true,
			},
		},
	},
	NewspaperPage{
		Title:  "Buylist Growth - 7 Day Forecast",
		Desc:   "Forecasting Card Kingdom's Buylist Offers on Cards",
		Option: "buylist_growth",
	},
	NewspaperPage{
		Title:  "Buylist Forecast - Performance Review",
		Desc:   "Comparing the Buylist forecasts from a week ago with current, to provide additional context of how well one might expect them to perform moving forward",
		Option: "buylist_perf",
	},
	NewspaperPage{
		Title:  "Vendor Growth - 7 Day Forecast",
		Desc:   "Forecasting TCG Vendor Levels for Individual Cards",
		Option: "vendor_forecast",
	},
	NewspaperPage{
		Title:  "Vendor Forecast - Performance Review",
		Desc:   "Comparing the TCG Vendor forecasts from a week ago with current, to provide additional context of how well one might expect them to perform moving forward",
		Option: "vendor_growth",
	},
}

func Newspaper(w http.ResponseWriter, r *http.Request) {
	sig := r.FormValue("sig")

	pageVars := genPageNav("Newspaper", sig)

	if !DatabaseLoaded {
		pageVars.Title = "Great things are coming"
		pageVars.ErrorMessage = "Website is starting, please try again in a few minutes"

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
	var query, defSort string

	if page == "" {
		pageVars.Title = "Index"

		render(w, "news.html", pageVars)

		return
	}

	for _, newspage := range NewspaperPages {
		if newspage.Option == page {
			pageVars.Page = newspage.Option
			pageVars.Title = newspage.Title
			pageVars.InfoMessage = newspage.Desc
			pageVars.Headings = newspage.Head

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
	// Keep things limited
	query = fmt.Sprintf("%s LIMIT %d", query, newsPageSize)

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
	uuids := mtgmatcher.GetUUIDs()
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

		uuid := result[1]
		co, found := uuids[uuid]
		if !found {
			log.Println(uuid, "not found")
			continue
		}

		// Load card data
		pageVars.Cards = append(pageVars.Cards, GenericCard{
			Name:     co.Card.Name,
			Edition:  co.Edition,
			SetCode:  co.SetCode,
			Number:   co.Card.Number,
			Keyrune:  keyruneForCardSet(uuid),
			ImageURL: fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=small", strings.ToLower(co.SetCode), co.Card.Number),
			Reserved: co.Card.IsReserved,
		})

		// Allocate a table row with as many fields as returned by the SELECT
		// minus the two well-known fields
		pageVars.Table[i] = make([]string, len(result)-2)
		for j := range pageVars.Table[i] {
			pageVars.Table[i][j] = result[2+j]
		}

		// Next row!
		i++
	}

	for _, c := range pageVars.Cards {
		if c.Reserved {
			pageVars.HasReserved = true
			break
		}
	}

	if len(pageVars.Cards) == 0 {
		pageVars.InfoMessage = "Newspaper is on strike"
	}

	render(w, "news.html", pageVars)
}

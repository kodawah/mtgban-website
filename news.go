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
		Desc:   "Rankings are weighted via prior 21, 15, and 7 days via Retail, Buy list, and several other criteria to arrive at an overall ranking",
		Option: "review",
		Query:  "SELECT * FROM top_25",
		Sort:   "Ranking",
		Head: []Heading{
			Heading{
				Title:   "Ranking",
				CanSort: true,
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
				IsDollar: true,
			},
			Heading{
				Title:    "Buylist",
				CanSort:  true,
				IsDollar: true,
			},
			Heading{
				Title:   "Vendors",
				CanSort: true,
			},
		},
	},
	NewspaperPage{
		Title:  "Greatest Decrease in Vendor Listings",
		Desc:   "Information Sourced from TCG: Stock decreases indicate that there is not enough supply to meet current demand across the reviewed time period (tl:dr - Seek these out)",
		Option: "stock_dec",
	},
	NewspaperPage{
		Title:  "Greatest Increase in Vendor Listings",
		Desc:   "Information Sourced from TCG: Stock Increases indicate that there is more than enough supply to meet current demand across the reviewed time period (tl:dr - Avoid These)",
		Option: "stock_inc",
	},
	NewspaperPage{
		Title:  "Greatest Increase in Buy List Offer",
		Desc:   "Information Sourced from CK: Buy List increases indicate a higher sales rate (eg. higher demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Option: "buylist_inc",
	},
	NewspaperPage{
		Title:  "Greatest Decrease in Buy List Offer",
		Desc:   "Information Sourced from CK: Buy List Decreases indicate a declining sales rate (eg, Less demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Option: "buylist_dec",
	},
	NewspaperPage{
		Title:  "Buy List Growth - 7 Day Forecast",
		Desc:   "Forecasting Card Kingdom's Buy List Offers on Cards",
		Option: "buylist_growth",
	},
	NewspaperPage{
		Title:  "Buy List Forecast - Performance Review",
		Desc:   "Comparing the Buy List forecasts from a week ago with current, to provide additional context of how well one might expect them to perform moving forward",
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
			if head.Title == sort {
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

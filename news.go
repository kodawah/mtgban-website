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

type Top25 struct {
	RowNames string
	UUID     string
	Ranking  int
	Retail   float64
	Buylist  float64
	Vendors  sql.NullInt64
}

type GenericCard struct {
	Name     string
	Edition  string
	SetCode  string
	Number   string
	Keyrune  string
	ImageURL string
	Reserved bool
}

type Top25List struct {
	Ranking int
	Retail  float64
	Buylist float64
	Vendors int
}

const (
	newsPageSize = 25
	newsBaseURL  = "/newspaper?page="
)

type NewspaperPage struct {
	Title string
	Desc  string
	Nav   NavElem
}

var NewspaperPages = []NewspaperPage{
	NewspaperPage{
		Title: "Top 25 Singles (3 Week Market Review)",
		Desc:  "Rankings are weighted via prior 21, 15, and 7 days via Retail, Buy list, and several other criteria to arrive at an overall ranking",
		Nav: NavElem{
			Name:  "Market Review",
			Short: "review",
		},
	},
	NewspaperPage{
		Title: "Greatest Decrease in Vendor Listings",
		Desc:  "Information Sourced from TCG: Stock decreases indicate that there is not enough supply to meet current demand across the reviewed time period (tl:dr - Seek these out)",
		Nav: NavElem{
			Name:  "Stock Decreases",
			Short: "stock_dec",
		},
	},
	NewspaperPage{
		Title: "Greatest Increase in Vendor Listings",
		Desc:  "Information Sourced from TCG: Stock Increases indicate that there is more than enough supply to meet current demand across the reviewed time period (tl:dr - Avoid These)",
		Nav: NavElem{
			Name:  "Stock Increases",
			Short: "stock_inc",
		},
	},
	NewspaperPage{
		Title: "Greatest Increase in Buy List Offer",
		Desc:  "Information Sourced from CK: Buy List increases indicate a higher sales rate (eg. higher demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Nav: NavElem{
			Name:  "Buylist Increases",
			Short: "buylist_inc",
		},
	},
	NewspaperPage{
		Title: "Greatest Decrease in Buy List Offer",
		Desc:  "Information Sourced from CK: Buy List Decreases indicate a declining sales rate (eg, Less demand). These may be fleeting, do not base a purchase solely off this metric unless dropshipping",
		Nav: NavElem{
			Name:  "Buylist Decreases",
			Short: "buylist_dec",
		},
	},
	NewspaperPage{
		Title: "Buy List Growth - 7 Day Forecast",
		Desc:  "Forecasting Card Kingdom's Buy List Offers on Cards",
		Nav: NavElem{
			Name:  "Buylist Growth Forecast",
			Short: "buylist_growth",
		},
	},
	NewspaperPage{
		Title: "Buy List Forecast - Performance Review",
		Desc:  "Comparing the Buy List forecasts from a week ago with current, to provide additional context of how well one might expect them to perform moving forward",
		Nav: NavElem{
			Name:  "Buylist Performance Forecast",
			Short: "buylist_perf",
		},
	},
	NewspaperPage{
		Title: "Vendor Growth - 7 Day Forecast",
		Desc:  "Forecasting TCG Vendor Levels for Individual Cards",
		Nav: NavElem{
			Name:  "Vendor Forecast",
			Short: "vendor_forecast",
		},
	},
	NewspaperPage{
		Title: "Vendor Forecast - Performance Review",
		Desc:  "Comparing the TCG Vendor forecasts from a week ago with current, to provide additional context of how well one might expect them to perform moving forward",
		Nav: NavElem{
			Name:  "Vendor Growth Forecast",
			Short: "vendor_growth",
		},
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
	// TODO check for 3day or 1day newspaper
	enabled, _ := GetParamFromSig(sig, "type")
	if enabled == "ALL" {
	} else if enabled == "DEFAULT" {
	}

	r.ParseForm()
	page := r.FormValue("page")

	extraNav := make([]NavElem, 0, len(NewspaperPages))
	for _, newspage := range NewspaperPages {
		nav := newspage.Nav
		nav.Link = newsBaseURL + nav.Short
		if sig != "" {
			nav.Link += "&sig=" + sig
		}

		if nav.Short == page {
			nav.Active = true
			nav.Class = "selected"
			pageVars.Title = newspage.Title
			pageVars.InfoMessage = newspage.Desc
		}

		extraNav = append(extraNav, nav)
	}

	pageVars.Nav = insertNavBar("Newspaper", pageVars.Nav, extraNav)

	if page == "" {
		pageVars.Title = "Instructions"
	} else if page != "review" {
		pageVars.Title = "Under construction"
		render(w, "news.html", pageVars)
		return
	}

	pageVars.Cards = make([]GenericCard, 0, newsPageSize)
	pageVars.Top25 = make([]Top25List, 0, newsPageSize)

	results, err := NewspaperDB.Query("SELECT * FROM top_25 LIMIT ?", newsPageSize)
	// ORDER BY retail/etc DESC/ASC
	if err != nil {
		log.Println(err)
		return
	}

	uuids := mtgmatcher.GetUUIDs()
	for results.Next() {
		var row Top25
		err := results.Scan(&row.RowNames, &row.UUID, &row.Ranking, &row.Retail, &row.Buylist, &row.Vendors)
		if err != nil {
			log.Println(err)
			continue
		}

		co, found := uuids[row.UUID]
		if !found {
			log.Println(row.UUID, "not found")
			continue
		}

		pageVars.Cards = append(pageVars.Cards, GenericCard{
			Name:     co.Card.Name,
			Edition:  co.Edition,
			SetCode:  co.SetCode,
			Number:   co.Card.Number,
			Keyrune:  keyruneForCardSet(row.UUID),
			ImageURL: fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=small", strings.ToLower(co.SetCode), co.Card.Number),
			Reserved: co.Card.IsReserved,
		})
		pageVars.Top25 = append(pageVars.Top25, Top25List{
			Ranking: row.Ranking,
			Retail:  row.Retail,
			Buylist: row.Buylist,
			Vendors: int(row.Vendors.Int64),
		})
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

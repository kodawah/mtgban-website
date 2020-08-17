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

	//todo: list all pages
	/*	for _, newSeller := range Sellers {
		nav := NavElem{
			Name:  newSeller.Info().Name,
			Short: newSeller.Info().Shorthand,
			Link:  "/newspaper?page=" + newSeller.Info().Shorthand,
		}

		if sig != "" {
			nav.Link += "&sig=" + sig
		}

		pageVars.Nav = append(pageVars.Nav, nav)
		break
	}*/

	pageVars.Title = "Newspaper"

	pageVars.Title = "Top 25 Singles (3 Week Market Review)"
	pageVars.InfoMessage = "Rankings are weighted via prior 21,15, and 7 days via Retail, Buy list, and several other criteria to arrive at an overall ranking"

	pageVars.Cards = []GenericCard{}
	pageVars.Top25 = []Top25List{}

	results, err := NewspaperDB.Query("SELECT * FROM top_25 LIMIT 25")
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

	if len(pageVars.Cards) == 0 {
		pageVars.InfoMessage = "Newspaper is on strike"
	}

	render(w, "news.html", pageVars)
}

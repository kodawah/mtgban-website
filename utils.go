package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-cleanhttp"

	"github.com/kodabb/go-mtgban/mtgmatcher"
	"github.com/kodabb/go-mtgban/mtgmatcher/mtgjson"
)

var Country2flag = map[string]string{
	"EU": "🇪🇺",
	"JP": "🇯🇵",
}

type GenericCard struct {
	Name      string
	Edition   string
	SetCode   string
	Number    string
	Variant   string
	Keyrune   string
	ImageURL  string
	Foil      bool
	Etched    bool
	Reserved  bool
	Title     string
	SearchURL string
	SypList   bool
	Stocks    bool
	StocksURL string
	Printings string
	TCGId     string
}

func fileExists(filename string) bool {
	fi, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		link, err := os.Readlink(filename)
		if err != nil {
			return false
		}
		fi, err = os.Stat(link)
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
		return !fi.IsDir()
	}
	return !fi.IsDir()
}

func fileDate(filename string) time.Time {
	fi, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return time.Now()
	}
	return fi.ModTime()
}

func mkDirIfNotExisting(dirName string) error {
	_, err := os.Stat(dirName)
	if errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(dirName, 0700)
	}
	return err
}

func keyruneForCardSet(cardId string) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	set, err := mtgmatcher.GetSet(co.Card.SetCode)
	if err != nil {
		return ""
	}

	keyrune := set.KeyruneCode

	out := "ss-" + strings.ToLower(keyrune)
	rarity := co.Card.Rarity
	if rarity == "special" || co.Etched {
		rarity = "timeshifted"
	} else if rarity == "token" || rarity == "oversize" {
		rarity = "common"
	}

	// Skip setting rarity for common, so that a color is not forcefully set
	// on the symbol, and can become white on a dark theme
	if rarity != "common" {
		out += " ss-" + rarity
	}

	if co.Foil {
		out += " ss-foil ss-grad"
	}

	return out
}

func scryfallImageURL(cardId string, small bool) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	if co.Sealed {
		return "https://product-images.tcgplayer.com/" + co.Identifiers["tcgplayerProductId"] + ".jpg"
	}

	version := "normal"
	if small {
		version = "small"
	}

	// Support BAN's custom sets
	number := co.Card.Number
	code := strings.ToLower(co.SetCode)
	if strings.HasSuffix(code, "ita") {
		code = strings.TrimSuffix(code, "ita")
		number += "/it"
	} else if strings.HasSuffix(code, "jpn") {
		code = strings.TrimSuffix(code, "jpn")
		number += "/ja"
	} else if strings.HasSuffix(code, "alt") {
		code = strings.TrimSuffix(code, "alt")
	}

	return fmt.Sprintf("https://api.scryfall.com/cards/%s/%s?format=image&version=%s", code, number, version)
}

func editionTitle(cardId string) string {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return ""
	}

	edition := co.Edition
	if co.OriginalReleaseDate != "" {
		edition = fmt.Sprintf("%s (%s)", edition, co.OriginalReleaseDate)
	}

	finish := ""
	if co.Etched {
		finish = " Etched"
	} else if co.Foil {
		finish = " Foil"
	}

	num := ""
	if !co.Sealed {
		num = "#" + co.Card.Number
	}

	return fmt.Sprintf("%s -%s %s %s", edition, finish, strings.Title(co.Card.Rarity), num)
}

func insertNavBar(page string, nav []NavElem, extra []NavElem) []NavElem {
	out := make([]NavElem, len(nav)+len(extra))
	var j int
	for i := range nav {
		out[j] = nav[i]
		if out[j].Name == page {
			for e := range extra {
				j++
				out[j] = extra[e]
			}
		}
		j++
	}
	return out
}

const (
	// 9 per line for default size, otherwise 19-21 depending on size
	MaxBeforeShrink = 18

	// After this amount just stop adding symbols
	MaxRuneSymbols = 57
)

func uuid2card(cardId string, flags ...bool) GenericCard {
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return GenericCard{}
	}

	var stocksURL string
	_, sypList := Infos["TCGSYPList"][cardId]
	_, stocks := Infos["STKS"][cardId]
	entries, found := Infos["STKSIndex"][cardId]
	if found {
		stocksURL = entries[0].URL
	}

	variant := ""
	if co.HasPromoType(mtgjson.PromoTypeBoosterfun) {
		switch {
		case co.HasFrameEffect(mtgjson.FrameEffectShowcase):
			variant = "Showcase "
		case co.HasFrameEffect(mtgjson.FrameEffectExtendedArt):
			variant = "Extended Art "
		case co.BorderColor == mtgjson.BorderColorBorderless:
			variant = "Borderless "
		}
	}
	// Loop through the supported promo types, skipping Boosterfun already processed above
	for _, promoType := range co.PromoTypes {
		if SliceStringHas(mtgjson.AllPromoTypes, promoType) && promoType != mtgjson.PromoTypeBoosterfun {
			variant += strings.Title(promoType) + " "
		}
	}
	variant = strings.TrimSpace(variant)

	isJPN := false
	switch co.SetCode {
	case "WAR":
		isJPN = strings.Contains(co.Card.Number, "★")
	case "STA":
		num, _ := strconv.Atoi(co.Number)
		isJPN = num > 63
		// Strip "Boderless"
		variant = ""
	case "PAFR":
		if strings.HasSuffix(co.Number, "a") {
			variant = "Ampersand"
		}
	case "MH2", "H1R":
		if co.FrameVersion == "1997" {
			variant = "Retro Frame"
		} else if co.HasFrameEffect(mtgjson.FrameEffectShowcase) {
			variant = "Sketch"
		}
	}

	if isJPN {
		if variant != "" {
			variant = " " + variant
		}
		variant = "JPN" + variant
	}

	query := co.Name
	if !co.Sealed {
		query = fmt.Sprintf("%s s:%s cn:%s", co.Name, co.SetCode, co.Number)
		if co.Etched {
			query += " f:etched"

			// Append Etched information to the tag
			if variant != "" {
				variant += " "
			}
			variant += "Etched"
		} else if co.Foil {
			query += " f:foil"
		} else if !co.Etched && !co.Foil {
			query += " f:nonfoil"
		}
	}

	smallImg := false
	if len(flags) > 0 {
		smallImg = flags[0]
	}
	printings := ""
	if len(flags) > 1 && flags[1] {
		// Hack to generate HTML in the template
		for i, setCode := range co.Printings {
			set, found := mtgmatcher.GetSets()[setCode]
			if !found {
				continue
			}
			keyruneCode := strings.ToLower(set.KeyruneCode)
			printings += fmt.Sprintf("<a class='pagination' title='%s' href='/search?q=%s'><i class='ss ss-%s ss-2x'></i> </a>", set.Name, url.QueryEscape(co.Name+" s:"+setCode), keyruneCode)
			if i == MaxRuneSymbols && len(co.Printings) > MaxRuneSymbols {
				printings += "<br>and many more (too many to list)..."
				break
			}
		}
		// Shrink icons to fit more of them
		if len(co.Printings) > MaxBeforeShrink {
			printings = strings.Replace(printings, "ss-2x", "ss-1x", -1)
		}
	}

	if co.Sealed {
		// The first chunk is always present, even for foil-only sets
		printings = "<h6>Set Value</h6><table class='setValue'>"

		for i, title := range ProductTitles {
			entries, found := Infos[ProductKeys[i]][co.SetCode]
			if found {
				printings += fmt.Sprintf("<tr class='setValue'><td class='setValue'><h5>%s</h5></td><td>$ %.02f</td></tr>", title, entries[0].Price)
			}
		}
		printings += "</table>"

		// The second chunk is optional, check for the first key
		if len(Infos[ProductFoilKeys[0]][co.SetCode]) > 0 {
			printings += "<br>"
			printings += "<h6>Foil Set Value</h6><table class='setValue'>"

			for i, title := range ProductTitles {
				entries, found := Infos[ProductFoilKeys[i]][co.SetCode]
				if found {
					printings += fmt.Sprintf("<tr class='setValue'><td class='setValue'><h5>%s</h5></td><td>$ %.02f</td></tr>", title, entries[0].Price)
				}
			}
			printings += "</table>"
		}
	}

	path := "search"
	if co.Sealed {
		path = "sealed"
	}

	tcgId := co.Card.Identifiers["tcgplayerProductId"]
	if co.Etched {
		tcgId = co.Card.Identifiers["tcgplayerEtchedProductId"]
	}

	return GenericCard{
		Name:      co.Card.Name,
		Edition:   co.Edition,
		SetCode:   co.Card.SetCode,
		Number:    co.Card.Number,
		Variant:   variant,
		Foil:      co.Foil,
		Etched:    co.Etched,
		Keyrune:   keyruneForCardSet(cardId),
		ImageURL:  scryfallImageURL(cardId, smallImg),
		Title:     editionTitle(cardId),
		Reserved:  co.Card.IsReserved,
		SearchURL: fmt.Sprintf("/%s?q=%s", path, url.QueryEscape(query)),
		SypList:   sypList,
		Stocks:    stocks,
		StocksURL: stocksURL,
		Printings: printings,
		TCGId:     tcgId,
	}
}

func SliceStringHas(slice []string, probe string) bool {
	for i := range slice {
		if slice[i] == probe {
			return true
		}
	}
	return false
}

type Notification struct {
	Username string `json:"username"`
	Content  string `json:"content"`
}

// Log and send the notification for a user action
func ServerNotify(kind, message string, flags ...bool) {
	log.Println(message)
	if Config.DiscordNotifHook == "" {
		return
	}
	if len(flags) > 0 && flags[0] {
		message = "@here " + message
	}
	go notify(kind, message, Config.DiscordNotifHook)
}

// Only send the notification for a user action
func UserNotify(kind, message string, flags ...bool) {
	if Config.DiscordHook == "" {
		return
	}
	if len(flags) > 0 && flags[0] {
		message = "@here " + message
	}
	go notify(kind, message, Config.DiscordHook)
}

func notify(kind, message, hook string) {
	var payload Notification
	payload.Username = kind
	if DevMode {
		payload.Content = "[DEV] "
	}
	payload.Content += message

	reqBody, err := json.Marshal(&payload)
	if err != nil {
		log.Println(err)
		return
	}

	resp, err := cleanhttp.DefaultClient().Post(hook, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Println(err)
		return
	}
	resp.Body.Close()
}

// Read the query parameter, if present set a cookie that will be
// used as default preference, otherwise retrieve the said cookie
func readSetFlag(w http.ResponseWriter, r *http.Request, queryParam, cookieName string) bool {
	val := r.FormValue(queryParam)
	flag, err := strconv.ParseBool(val)
	if err != nil {
		flag, _ = strconv.ParseBool(readCookie(r, cookieName))
		return flag
	}
	setCookie(w, r, cookieName, val)
	return flag
}

// Read a cookie from the request
func readCookie(r *http.Request, cookieName string) string {
	for _, cookie := range r.Cookies() {
		if cookie.Name == cookieName {
			return cookie.Value
		}
	}
	return ""
}

// Set a cookie in the response with no expiration at the default root
func setCookie(w http.ResponseWriter, r *http.Request, cookieName, value string) {
	domain := "mtgban.com"
	if strings.Contains(getBaseURL(r), "localhost") {
		domain = "localhost"
	}
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Domain: domain,
		Path:   "/",
		// No expiration
		Expires: time.Now().Add(10 * 365 * 24 * 60 * 60 * time.Second),
		Value:   value,
		// Enforce first party cookies only
		SameSite: http.SameSiteStrictMode,
	})
	return
}

// Retrieve default blocklists according to the signature contents
func getDefaultBlocklists(sig string) ([]string, []string) {
	var blocklistRetail, blocklistBuylist []string
	blocklistRetailOpt := GetParamFromSig(sig, "SearchDisabled")
	if blocklistRetailOpt == "" {
		blocklistRetail = Config.SearchRetailBlockList
	} else if blocklistRetailOpt != "NONE" {
		blocklistRetail = strings.Split(blocklistRetailOpt, ",")
	}
	blocklistBuylistOpt := GetParamFromSig(sig, "SearchBuylistDisabled")
	if blocklistBuylistOpt == "" {
		blocklistBuylist = Config.SearchBuylistBlockList
	} else if blocklistBuylistOpt != "NONE" {
		blocklistBuylist = strings.Split(blocklistBuylistOpt, ",")
	}
	return blocklistRetail, blocklistBuylist
}

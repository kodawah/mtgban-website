package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	cleanhttp "github.com/hashicorp/go-cleanhttp"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

var poweredByFooter = discordgo.MessageEmbedFooter{
	IconURL: "https://www.mtgban.com/img/logo/ban-round.png",
	Text:    "Powered by mtgban.com",
}

// Scryfall-compatible mode
var squareBracketsRE = regexp.MustCompile(`\[\[.*?\]\]?`)

const (
	// Avoid making messages overly long
	MaxPrintings = 12

	// Overflow prevention for field.Value size
	MaxCustomEntries = 7

	// Discord API constants
	MaxEmbedFieldsValueLength = 1024
	MaxEmbedFieldsNumber      = 25

	// Timeout before cancelling a last sold price request
	LastSoldTimeout = 30

	// IDs of the channels on the main server
	DevChannelID   = "769323295526748160"
	RecapChannelID = "798588735259279453"
)

func setupDiscord() error {
	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + Config.DiscordToken)
	if err != nil {
		return err
	}

	// Register the guildCreate func as a callback for GuildCreat events
	dg.AddHandler(guildCreate)

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// In this example, we only care about receiving message events.
	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuilds | discordgo.IntentsGuildMessages)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		return err
	}

	return nil
	// Cleanly close down the Discord session.
	//dg.Close()
}

// This function will be called every time the bot is invited to a discord
// server and tries to join it.
func guildCreate(s *discordgo.Session, gc *discordgo.GuildCreate) {
	// Set a "is playing" status
	s.UpdateStatus(0, "http://mtgban.com")

	// If guild is authorized, then we can proceed as normal
	if stringSliceContains(Config.DiscordAllowList, gc.Guild.ID) {
		return
	}
	// Skip this check when running on dev
	if DevMode {
		return
	}

	// Otherwise we print a message, pick our stuff, and leave
	s.ChannelMessageSendEmbed(gc.Guild.SystemChannelID, &discordgo.MessageEmbed{
		Description: "Looks like I'm not authorized to be here ⋋〳 ᵕ _ʖ ᵕ 〵⋌",
		Footer:      &poweredByFooter,
	})
	Notify("bot", gc.Guild.Name+" attempted to install me ▐ ✪ _ ✪▐")
	s.GuildLeave(gc.Guild.ID)
}

type searchResult struct {
	Invalid         bool
	CardId          string
	ResultsIndex    []SearchEntry
	ResultsSellers  []SearchEntry
	ResultsVendors  []SearchEntry
	EditionSearched string
	SearchQuery     string
}

var filteredEditions = []string{
	"PTC",
	"WC00",
	"WC01",
	"WC02",
	"WC03",
	"WC04",
	"WC97",
	"WC98",
	"WC99",
}

func parseMessage(content string) (*searchResult, error) {
	// Clean up query and only search for NM
	query, options := parseSearchOptions(content)

	// Filter out any undersirable sets, unless explicitly requested
	filterGoldOut := true
	if options["edition"] != "" {
		if SliceStringHas(filteredEditions, options["edition"]) {
			filterGoldOut = false
		}
	}
	if filterGoldOut {
		options["not_edition"] = strings.Join(filteredEditions, ",")
	}

	// Stash interesting elements that may be overwritten later
	parsedCondition := options["condition"]

	// Prevent useless invocations
	if len(query) < 3 && query != "Ow" && query != "X" {
		return &searchResult{Invalid: true}, nil
	}

	// Check if card exists
	var nameFound string
	sets := mtgmatcher.GetSets()
	if options["edition"] != "" {
		code := strings.Split(options["edition"], ",")[0]
		set, found := sets[code]
		if !found {
			return nil, fmt.Errorf("No edition found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", code)
		}
		for _, card := range set.Cards {
			if mtgmatcher.Contains(card.Name, query) {
				nameFound = card.Name
				break
			}
		}
		if nameFound == "" {
			return nil, fmt.Errorf("No card found named \"%s\" in %s 乁| ･ิ ∧ ･ิ |ㄏ", query, set.Name)
		}
	}
	if nameFound == "" {
		for _, set := range sets {
			for _, card := range set.Cards {
				if mtgmatcher.Contains(card.Name, query) {
					nameFound = card.Name
					break
				}
			}
			if nameFound != "" {
				break
			}
		}
	}
	if nameFound == "" {
		return nil, fmt.Errorf("No card found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", query)
	}

	// We can be quite sure that one of the index will contain the card requested,
	// so we translate the result into a new query to feed to the other searches
	cardId, resultsIndex, err := searchSellersFirstResult(query, options, true)
	if err != nil {
		return nil, err
	}
	query, options = parseSearchOptions(cardId)

	// Restore elements originally present in the first query
	options["condition"] = parsedCondition

	// Search both sellers and vendors
	var resultsSellers, resultsVendors []SearchEntry
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		_, resultsSellers, _ = searchSellersFirstResult(query, options, false)
		wg.Done()
	}()
	go func() {
		_, resultsVendors, _ = searchVendorsFirstResult(query, options)
		wg.Done()
	}()

	wg.Wait()

	// Rebuild the search query
	searchQuery := nameFound
	if options["edition"] != "" {
		searchQuery += " s:" + options["edition"]
	}
	if options["number"] != "" {
		searchQuery += " cn:" + options["number"]
	}
	if options["foil"] != "" {
		searchQuery += " f:" + options["foil"]
	}

	return &searchResult{
		CardId:          cardId,
		ResultsIndex:    resultsIndex,
		ResultsSellers:  resultsSellers,
		ResultsVendors:  resultsVendors,
		EditionSearched: options["edition"],
		SearchQuery:     searchQuery,
	}, nil
}

type embedField struct {
	Name   string
	Value  string
	Inline bool
}

func search2fields(searchRes *searchResult) (fields []embedField) {
	// Add two embed fields, one for retail and one for buylist
	fieldsNames := []string{
		"Index", "Retail", "Buylist",
	}
	for i, results := range [][]SearchEntry{
		searchRes.ResultsIndex, searchRes.ResultsSellers, searchRes.ResultsVendors,
	} {
		field := embedField{
			Name: fieldsNames[i],
		}
		if fieldsNames[i] != "Index" {
			field.Inline = true
		}

		// Results look really bad after MaxCustomEntries, and too much info
		// does not help, so sort by best price, trim, then sort back to original
		if len(results) > MaxCustomEntries {
			if fieldsNames[i] == "Retail" {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price < results[j].Price
				})
			} else if fieldsNames[i] == "Buylist" {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price > results[j].Price
				})
			}
			results = results[:MaxCustomEntries]
			sort.Slice(results, func(i, j int) bool {
				return results[i].ScraperName < results[j].ScraperName
			})
		}

		// Alsign to the longest name by appending whitespaces
		alignLength := longestName(results)
		for _, entry := range results {
			extraSpaces := ""
			for i := len(entry.ScraperName); i < alignLength; i++ {
				extraSpaces += " "
			}

			// Build url for our redirect
			kind := strings.ToLower(string(fieldsNames[i][0]))
			store := strings.Replace(entry.Shorthand, " ", "%20", -1)
			link := "https://" + DefaultHost + "/" + path.Join("go", kind, store, searchRes.CardId)

			// Set the custom field
			value := fmt.Sprintf("• **[`%s%s`](%s)** $%0.2f", entry.ScraperName, extraSpaces, link, entry.Price)
			if entry.Ratio > 60 {
				value += " 🔥"
			}
			if fieldsNames[i] == "Index" {
				// Split the Value string so that we can edit each of them separately
				subs := strings.Split(field.Value, "\n")
				// Determine which index we're merging
				tag := strings.Fields(entry.ScraperName)[0]
				// Merge status, normally just add the price
				merged := false
				for j := range subs {
					// Check what kind of replacement needs to be done
					if strings.Contains(subs[j], tag) {
						// Adjust the name
						if tag == "TCG" {
							subs[j] = strings.Replace(subs[j], "TCG Low", "TCG (Low/Market)", 1)
						} else if tag == "MKM" {
							subs[j] = strings.Replace(subs[j], "MKM Low", "MKM (Low/Trend) ", 1)
						}
						// Append the other price
						subs[j] += fmt.Sprintf(" / $%0.2f", entry.Price)
						merged = true
					}
				}
				if merged {
					// Rebuild the Value and move to the next item
					field.Value = strings.Join(subs, "\n")
					continue
				}
				value = fmt.Sprintf("• **[`%s`](%s)** $%0.2f", entry.ScraperName, link, entry.Price)
			} else if fieldsNames[i] == "Buylist" {
				alarm := false
				for _, subres := range searchRes.ResultsSellers {
					// Skip non-NM results
					if strings.HasSuffix(subres.ScraperName, "P)") {
						continue
					}
					// 90% of sell price is the minimum for arbit
					if subres.Price < entry.Price*0.9 {
						alarm = true
						break
					}
				}
				if alarm {
					value += " 🚨"
				}
			}
			value += "\n"

			// If we go past the maximum value for embed field values,
			// make a new field for any spillover, as long as we are within
			// the limits of the number of embeds allowed
			if len(field.Value)+len(value) > MaxEmbedFieldsValueLength && len(fields) < MaxEmbedFieldsNumber {
				fields = append(fields, field)
				field = embedField{
					Name:   fieldsNames[i] + " (cont'd)",
					Inline: true,
				}
			}
			field.Value += value
		}
		if len(results) == 0 {
			field.Value = "N/A"
		}

		fields = append(fields, field)
	}

	return
}

type PriceEntry struct {
	Title    string  `json:"title"`
	Price    float64 `json:"price"`
	Shipping float64 `json:"shipping"`
}

func grabLastSold(tcgId string, foil bool) ([]embedField, error) {
	if tcgId == "" {
		return nil, errors.New("empty tcgId")
	}

	resp, err := cleanhttp.DefaultClient().Get("http://localhost:8081/" + tcgId)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var entries map[string][]PriceEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		log.Println(string(data))
		return nil, err
	}

	var fields []embedField
	var shipping []string
	var hasValues bool
	for i, entry := range entries["TCG Last Sold Listing"] {
		if foil && !strings.Contains(entry.Title, "Foil") {
			continue
		}

		value := "-"
		if entry.Price != 0 {
			hasValues = true
			value = fmt.Sprintf("$%0.2f", entry.Price)
			shipping = append(shipping, fmt.Sprintf("%0.2f", entry.Shipping))
		}
		fields = append(fields, embedField{
			Name:   entry.Title,
			Value:  value,
			Inline: true,
		})

		if i == 4 || i == 9 {
			field := embedField{
				Name:   "Shipping",
				Value:  strings.Join(shipping, " "),
				Inline: true,
			}
			if field.Value == "" {
				field.Value = "n/a"
			}
			fields = append(fields, field)
			shipping = []string{}
		}
	}

	// No prices received, this is not an error,
	// but print a message warning the user
	if !hasValues {
		log.Println("No last sold prices available")
		return nil, nil
	}

	return fields, nil
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore requests if starting up
	if !DatabaseLoaded {
		return
	}

	// Ignore messages coming from unauthorized discords
	if !stringSliceContains(Config.DiscordAllowList, m.GuildID) {
		return
	}

	// Ignore all messages created by a bot
	if m.Author.Bot {
		return
	}

	// Ignore too short messages
	if len(m.Content) < 2 {
		return
	}

	// Ingore messages not coming from the test channel when running in dev
	if DevMode && m.ChannelID != DevChannelID {
		return
	}

	// Parse message, look for bot command
	if !strings.HasPrefix(m.Content, "!") && !strings.HasPrefix(m.Content, "$$") {
		// Early exit if not running on the main server
		if m.GuildID != Config.DiscordAllowList[0] {
			return
		}

		// Check if selected channels can replace scryfall searches
		if (m.ChannelID == DevChannelID || m.ChannelID == RecapChannelID) &&
			strings.Contains(m.Content, "[[") {
			fields := squareBracketsRE.FindAllString(m.Content, -1)
			for _, field := range fields {
				m.Content = "!" + strings.TrimRight(strings.TrimLeft(field, "["), "]")
				messageCreate(s, m)
			}
			// Check if the message contains potential links
		} else if strings.Contains(m.Content, "cardkingdom.com/mtg") ||
			strings.Contains(m.Content, "shop.tcgplayer.com/") {
			// Iterate over each segment of the message and look for known links
			fields := strings.Fields(m.Content)
			for _, field := range fields {
				if !strings.Contains(field, "cardkingdom.com/mtg") &&
					!strings.Contains(field, "shop.tcgplayer.com/") {
					continue
				}
				u, err := url.Parse(field)
				if err != nil {
					continue
				}
				// Check if there is not an affiliate already
				v := u.Query()
				if v.Get("partner") != "" {
					continue
				}

				// Flags for later use
				isCK := strings.Contains(field, "cardkingdom.com/mtg")
				isTCG := strings.Contains(field, "shop.tcgplayer.com/")

				// Add the MTGBAN affiliation (same id for tcg and ck)
				commonTag := Config.Affiliate["CK"]
				v.Set("partner", commonTag)
				v.Set("utm_source", commonTag)
				if isCK {
					v.Set("utm_campaign", commonTag)
					v.Set("utm_medium", "affiliate")
				} else if isTCG {
					v.Set("utm_campaign", "affliate")
					v.Set("utm_medium", commonTag)
				}
				u.RawQuery = v.Encode()

				// Extract a sensible link title
				title := strings.Title(strings.Replace(path.Base(u.Path), "-", " ", -1))
				if isCK {
					title += " at Card Kingdom"
				} else if isTCG {
					if title == "Productsearch" {
						title = "Your search"
					}
					title += " at TCGplayer"
				}
				// Spam time!
				_, err = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Title:       title,
					URL:         u.String(),
					Description: "Support **MTGBAN** by using this link",
				})
				if err != nil {
					log.Println(err)
				}
			}
		}
		return
	}

	allBls := strings.HasPrefix(m.Content, "!")
	lastSold := strings.HasPrefix(m.Content, "$$")

	// Strip away beginning character
	content := strings.TrimPrefix(m.Content, "!")
	content = strings.TrimPrefix(content, "$$")

	// Search a single card match
	searchRes, err := parseMessage(content)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Description: err.Error(),
		})
		return
	}
	if searchRes.Invalid {
		return
	}

	co, err := mtgmatcher.GetUUID(searchRes.CardId)
	if err != nil {
		return
	}

	var ogFields []embedField
	var channel chan *discordgo.MessageEmbed

	if allBls {
		ogFields = search2fields(searchRes)
	} else if lastSold {
		// Since grabLastSold is slow, spawn a goroutine and wait for the real
		// results later, after posting a "please wait" message
		go func() {
			channel = make(chan *discordgo.MessageEmbed)
			var errMsg string
			ogFields, err = grabLastSold(co.Identifiers["tcgplayerProductId"], co.Foil)
			if err != nil {
				errMsg = "Internal bot error, please notify devs ┏༼ ◉ ╭╮ ◉༽┓"
				log.Println("Bot error:", err, "from", content)
			} else if len(ogFields) == 0 {
				errMsg = "No Last Sold Price available for \"" + content + "\" o͡͡͡╮༼ • ʖ̯ • ༽╭o͡͡͡"
			}
			embed := prepareCard(searchRes, ogFields, m.GuildID, lastSold)
			if errMsg != "" {
				embed.Description += errMsg
			}
			channel <- embed
		}()
	}

	embed := prepareCard(searchRes, ogFields, m.GuildID, lastSold)
	if lastSold {
		embed.Description += "Grabbing last sold prices, hang tight ᕕ( ՞ ᗜ ՞ )ᕗ"
	}

	out, err := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	if err != nil {
		log.Println(err)
		return
	}
	if lastSold {
		var edit *discordgo.MessageEmbed

		// Either get the result from the channel or time out
		select {
		case edit = <-channel:
			break
		case <-time.After(LastSoldTimeout * time.Second):
			edit = prepareCard(searchRes, ogFields, m.GuildID, lastSold)
			edit.Description += "Connection time out (-, – )…zzzZZZ"
			break
		}

		_, err = s.ChannelMessageEditEmbed(m.ChannelID, out.ID, edit)
		if err != nil {
			log.Println(err)
		}
	}
}

func prepareCard(searchRes *searchResult, ogFields []embedField, guildId string, lastSold bool) *discordgo.MessageEmbed {
	// Convert search results into proper fields
	var fields []*discordgo.MessageEmbedField
	for _, field := range ogFields {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   field.Name,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}

	// Prepare card data
	card := uuid2card(searchRes.CardId, true)
	co, _ := mtgmatcher.GetUUID(searchRes.CardId)

	printings := strings.Join(co.Printings, ", ")
	if len(co.Printings) > MaxPrintings {
		printings = strings.Join(co.Printings[:MaxPrintings], ", ") + " and more"
	}
	if searchRes.EditionSearched != "" && len(co.Variations) > 0 {
		cn := []string{co.Number}
		for _, varid := range co.Variations {
			co, err := mtgmatcher.GetUUID(varid)
			if err != nil {
				continue
			}
			cn = append(cn, co.Number)
		}
		printings = fmt.Sprintf("%s. Variants in %s are %s", printings, searchRes.EditionSearched, strings.Join(cn, ", "))
	}

	link := "https://www.mtgban.com/search?q=" + url.QueryEscape(searchRes.SearchQuery) + "&utm_source=banbot&utm_affiliate=" + guildId

	// Set title of the main message
	title := "Prices for " + card.Name
	if lastSold {
		title = "TCG Last Sold prices for " + card.Name
	}

	// Add a tag for ease of debugging
	if DevMode {
		title = "[DEV] " + title
	}
	// Spark-ly
	if card.Foil {
		title += " ✨"
	}

	desc := fmt.Sprintf("[%s] %s\nPrinted in %s.\n\n", card.SetCode, card.Title, printings)

	embed := discordgo.MessageEmbed{
		Title:       title,
		Color:       0xFF0000,
		URL:         link,
		Description: desc,
		Fields:      fields,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: card.ImageURL,
		},
		Footer: &discordgo.MessageEmbedFooter{},
	}

	// Some footer action, RL, stocks, powered by
	if card.Reserved {
		embed.Footer.Text = "Part of the Reserved List\n"
	}
	_, stocks := Infos["STKS"][searchRes.CardId]
	if stocks {
		embed.Footer.Text += "On MTGStocks Interests page\n"
	}
	// Show data source on non-ban servers
	if len(Config.DiscordAllowList) > 0 && guildId != Config.DiscordAllowList[0] {
		embed.Footer.IconURL = poweredByFooter.IconURL
		embed.Footer.Text += poweredByFooter.Text
	}

	return &embed
}

// Obtain the length of the scraper with the longest name
func longestName(results []SearchEntry) (out int) {
	for _, entry := range results {
		probe := len(entry.ScraperName)
		if probe > out {
			out = probe
		}
	}
	return
}

// Retrieve cards from Sellers using the very first result
func searchSellersFirstResult(query string, options map[string]string, index bool) (cardId string, results []SearchEntry, err error) {
	// Search
	foundSellers, _ := searchSellers(query, append(Config.SearchBlockList, "TCG Direct"), options)
	if len(foundSellers) == 0 {
		err = errors.New("Out of stock everywhere ┻━┻ ヘ╰( •̀ε•́ ╰)")
		return
	}

	sortedKeysSeller := make([]string, 0, len(foundSellers))
	for cardId := range foundSellers {
		sortedKeysSeller = append(sortedKeysSeller, cardId)
	}
	if len(sortedKeysSeller) > 1 {
		sort.Slice(sortedKeysSeller, func(i, j int) bool {
			return sortSets(sortedKeysSeller[i], sortedKeysSeller[j])
		})
	}

	cardId = sortedKeysSeller[0]
	if index {
		results = foundSellers[cardId]["INDEX"]
	} else {
		founders := map[string]string{}
		// Query results with the known (ordered) conditions
		for _, cond := range []string{"NM", "SP", "MP", "HP"} {
			foundResults := foundSellers[cardId][cond]

			// Loop through the results, keep track of the precessed
			// elements in the map (and skip lower condition ones)
			for _, result := range foundResults {
				_, found := founders[result.ScraperName]
				if found {
					continue
				}
				founders[result.ScraperName] = cond
				// If not NM, add a small tag
				if cond != "NM" {
					result.ScraperName += " (" + cond + ")"
				}
				results = append(results, result)
			}
		}
	}

	if len(results) > 0 {
		// Drop duplicates by looking at the last one as they are alredy sorted
		tmp := append(results[:0], results[0])
		for i := range results {
			if results[i].ScraperName != tmp[len(tmp)-1].ScraperName {
				tmp = append(tmp, results[i])
			}
		}
		results = tmp
	}
	return
}

// Retrieve cards from Vendors using the very first result
func searchVendorsFirstResult(query string, options map[string]string) (cardId string, results []SearchEntry, err error) {
	foundVendors, _ := searchVendors(query, Config.SearchBlockList, options)
	if len(foundVendors) == 0 {
		err = errors.New("Nobody is buying that card ┻━┻ ヘ╰( •̀ε•́ ╰)")
		return
	}

	sortedKeysVendor := make([]string, 0, len(foundVendors))
	for cardId := range foundVendors {
		sortedKeysVendor = append(sortedKeysVendor, cardId)
	}
	if len(sortedKeysVendor) > 1 {
		sort.Slice(sortedKeysVendor, func(i, j int) bool {
			return sortSets(sortedKeysVendor[i], sortedKeysVendor[j])
		})
	}

	cardId = sortedKeysVendor[0]
	results = foundVendors[cardId]
	return
}

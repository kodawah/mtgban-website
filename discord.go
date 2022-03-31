package main

import (
	"fmt"
	"log"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/kodabb/go-mtgban/mtgmatcher"
	"github.com/kodabb/go-mtgban/tcgplayer"
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
	ChatChannelID  = "736007847560609794"
)

var DiscordRetailBlocklist []string

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

	DiscordRetailBlocklist = append(Config.SearchRetailBlockList, TCG_DIRECT_LOW)

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
	s.UpdateGameStatus(0, "http://mtgban.com")

	// If guild is authorized, then we can proceed as normal
	if SliceStringHas(Config.DiscordAllowList, gc.Guild.ID) {
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
	UserNotify("bot", gc.Guild.Name+" attempted to install the bot", true)
	log.Println("unauthorized installation attempt")
	s.GuildLeave(gc.Guild.ID)
}

type searchResult struct {
	Invalid         bool
	CardId          string
	ResultsIndex    []SearchEntry
	ResultsSellers  []SearchEntry
	ResultsVendors  []SearchEntry
	EditionSearched string
}

var filteredEditions = []string{
	"FBB",
	"LEGITA",
	"DRKITA",
	"RIN",
	"4BB",
	"CHRJPN",
	"PTC",
	"SUM",
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
	// Clean up query, no blocklist because we only need keys
	config := parseSearchOptionsNG(content, nil, nil)
	query := config.CleanQuery

	// Prevent useless invocations
	if len(query) < 3 && query != "Ow" && query != "X" {
		return &searchResult{Invalid: true}, nil
	}

	var editionSearched string
	// Filter out any undersirable sets, unless explicitly requested
	filterGoldOut := true
	for _, filter := range config.CardFilters {
		if filter.Name == "edition" {
			filterGoldOut = false
			editionSearched = filter.Values[0]
			break
		}
	}
	if filterGoldOut {
		config.CardFilters = append(config.CardFilters, FilterElem{
			Name:   "edition",
			Negate: true,
			Values: filteredEditions,
		})
	}

	uuids, err := searchAndFilter(config)
	if err != nil {
		// Not found again, let's provide a meaningful error
		if editionSearched != "" {
			set, err := mtgmatcher.GetSet(editionSearched)
			if err != nil {
				return nil, fmt.Errorf("No edition found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", editionSearched)
			}
			msg := fmt.Sprintf("No card found named \"%s\" in %s 乁| ･ิ ∧ ･ิ |ㄏ", query, set.Name)
			printings, err := mtgmatcher.Printings4Card(query)
			if err == nil {
				msg = fmt.Sprintf("%s\n\"%s\" is printed in %s.", msg, query, printings2line(printings))
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("No card found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", query)
	}

	if len(uuids) == 0 {
		return nil, fmt.Errorf("No results found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", query)
	}

	// Keep the first (most recent) result
	sort.Slice(uuids, func(i, j int) bool {
		return sortSets(uuids[i], uuids[j])
	})
	cardId := uuids[0]

	return &searchResult{
		CardId:          cardId,
		EditionSearched: editionSearched,
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
				// Handle alignment manually
				extraSpaces = ""
				// Split the Value string so that we can edit each of them separately
				subs := strings.Split(field.Value, "\n")
				// Determine which index we're merging
				tag := strings.Fields(entry.ScraperName)[0]
				// Merge status, normally just add the price
				merged := false
				for j := range subs {
					// Check what kind of replacement needs to be done
					if entry.ScraperName == TCG_DIRECT {
						extraSpaces = "      "
					} else if strings.Contains(subs[j], tag) {

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
				value = fmt.Sprintf("• **[`%s%s`](%s)** $%0.2f", entry.ScraperName, extraSpaces, link, entry.Price)
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
			// The very first item is allowed not to have entries
			if fieldsNames[i] == "Index" {
				continue
			}
		}

		fields = append(fields, field)
	}

	return
}

type TCGLastSold struct {
	PreviousPage string `json:"previousPage"`
	NextPage     string `json:"nextPage"`
	ResultCount  int    `json:"resultCount"`
	Data         []struct {
		Condition     string    `json:"condition"`
		Variant       string    `json:"variant"`
		Language      string    `json:"language"`
		Quantity      int       `json:"quantity"`
		Title         string    `json:"title"`
		ListingType   string    `json:"listingType"`
		PurchasePrice float64   `json:"purchasePrice"`
		ShippingPrice float64   `json:"shippingPrice"`
		OrderDate     time.Time `json:"orderDate"`
	} `json:"data"`
}

func grabLastSold(cardId string, lang string) ([]embedField, error) {
	var fields []embedField

	// Retrieve information about the card
	co, err := mtgmatcher.GetUUID(cardId)
	if err != nil {
		return nil, err
	}

	// Get the id for TCGPlayer, if missing exit quietly
	tcgId := co.Identifiers["tcgplayerProductId"]
	if tcgId == "" {
		return nil, nil
	}
	if co.Etched {
		id, found := co.Identifiers["tcgplayerEtchedProductId"]
		if found {
			tcgId = id
		}
	}

	tcgLastSoldResp, err := tcgplayer.TCGLatestSales(tcgId)
	if err != nil {
		return nil, err
	}

	var hasValues bool
	for _, entry := range tcgLastSoldResp.Data {
		// If the card requested is the foil version, skip any non-foil entry
		if co.Foil && entry.Variant != "Foil" {
			continue
		}

		// If language is requested, skip any language non matching it
		if lang != "" && entry.Language != lang {
			continue
		}

		value := "-"
		if entry.PurchasePrice != 0 {
			hasValues = true
			value = fmt.Sprintf("$%0.2f", entry.PurchasePrice)
			if entry.ShippingPrice != 0 {
				value += fmt.Sprintf(" (+$%0.2f)", entry.ShippingPrice)
			}
		}
		fields = append(fields, embedField{
			Name:   entry.OrderDate.Format("2006-01-02"),
			Value:  value,
			Inline: true,
		})

		if len(fields) > 5 {
			break
		}
	}

	// No prices received, this is not an error,
	// but print a message warning the user
	if !hasValues {
		log.Println("No last sold prices available for id", tcgId)
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
	if !SliceStringHas(Config.DiscordAllowList, m.GuildID) {
		return
	}

	// Ignore all messages created by a bot (except the ones from Scryfall)
	if m.Author.Bot && m.Author.Username != "Scryfall" {
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
		if (m.ChannelID == DevChannelID || m.ChannelID == RecapChannelID || m.ChannelID == ChatChannelID) &&
			strings.Contains(m.Content, "[[") {
			fields := squareBracketsRE.FindAllString(m.Content, -1)
			for _, field := range fields {
				m.Content = "!" + strings.TrimRight(strings.TrimLeft(field, "["), "]")
				messageCreate(s, m)
			}
			// Check if the message contains potential links
		} else if strings.Contains(m.Content, "cardkingdom.com/mtg") ||
			strings.Contains(m.Content, "coolstuffinc.com/page") ||
			strings.Contains(m.Content, "gatherer.wizards.com") ||
			strings.Contains(m.Content, "scryfall.com/card") ||
			strings.Contains(m.Content, "www.tcgplayer.com/product") ||
			(strings.Contains(m.Content, "shop.tcgplayer.com/") && !strings.Contains(m.Content, "shop.tcgplayer.com/seller")) {
			// Iterate over each segment of the message and look for known links
			fields := strings.Fields(m.Content)
			for _, field := range fields {
				if !strings.Contains(field, "cardkingdom.com/mtg") &&
					!strings.Contains(field, "coolstuffinc.com/page") &&
					!strings.Contains(field, "gatherer.wizards.com") &&
					!strings.Contains(field, "scryfall.com/card") &&
					!strings.Contains(field, "tcgplayer.com/") {
					continue
				}
				u, err := url.Parse(field)
				if err != nil {
					continue
				}

				// Flags for later use
				isCK := strings.Contains(field, "cardkingdom.com/mtg")
				isCSI := strings.Contains(field, "coolstuffinc.com/page")
				isTCG := strings.Contains(field, "tcgplayer.com/")
				isWotC := strings.Contains(field, "gatherer.wizards.com")
				isScry := strings.Contains(field, "scryfall.com/card")

				// Add the MTGBAN affiliation
				v := u.Query()
				switch {
				case isCSI:
					v.Set("utm_referrer", Config.Affiliate["CSI"])
				case isCK || isTCG:
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
				case isWotC:
					u2, err := url.Parse(field)
					if err != nil {
						continue
					}
					mid := u2.Query().Get("multiverseid")
					for _, co := range mtgmatcher.GetUUIDs() {
						if co.Identifiers["multiverseId"] == mid {
							m.Content = fmt.Sprintf("!%s|%s|%s", co.Name, co.SetCode, co.Number)
							messageCreate(s, m)
							return
						}
					}
				case isScry:
					u2, err := url.Parse(field)
					if err != nil {
						continue
					}
					paths := strings.Split(u2.Path, "/")
					if len(paths) > 4 {
						cardName := paths[4]
						setCode := paths[2]
						number := paths[3]

						m.Content = "!" + cardName
						_, err := mtgmatcher.GetSet(setCode)
						if err == nil {
							m.Content += "|" + setCode + "|" + number
						}
						messageCreate(s, m)
						return
					}
				}

				u.RawQuery = v.Encode()

				// Extract a sensible link title
				title := strings.Title(strings.Replace(path.Base(u.Path), "-", " ", -1))
				if isCK {
					title += " at Card Kingdom"
				} else if isTCG {
					// The old style links do not have the product id and have an extra element
					if strings.HasSuffix(u.Path, "/listing") {
						title = strings.Title(strings.Replace(path.Base(strings.TrimSuffix(u.Path, "/listing")), "-", " ", -1))
					}
					// Sometimes there is the product id embedded in the URL,
					// try to find it and use it to decorate the title
					productId := strings.TrimPrefix(u.Path, "/product/")

					slashIndex := strings.Index(productId, "/")
					if slashIndex != -1 {
						productId = productId[:slashIndex]
					}
					productId = mtgmatcher.Tcg2UUID(productId)
					co, err := mtgmatcher.GetUUID(productId)
					if err == nil {
						// Keep the edition first to mimic the normal style
						title = fmt.Sprintf("Magic %s %s", co.Edition, co.Name)
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
		config := parseSearchOptionsNG(searchRes.CardId, DiscordRetailBlocklist, Config.SearchBuylistBlockList)

		// Skip any store based outside of the US
		config.StoreFilters = append(config.StoreFilters, FilterStoreElem{
			Name:   "region_keep_index",
			Values: []string{"us"},
		})

		foundSellers, foundVendors := searchParallelNG(config)

		searchRes.ResultsIndex = processSellersResults(foundSellers, true)
		searchRes.ResultsSellers = processSellersResults(foundSellers, false)
		searchRes.ResultsVendors = processVendorsResults(foundVendors)

		ogFields = search2fields(searchRes)
	} else if lastSold {
		// Since grabLastSold is slow, spawn a goroutine and wait for the real
		// results later, after posting a "please wait" message
		go func() {
			channel = make(chan *discordgo.MessageEmbed)
			var errMsg string
			// Set a language for these sets, as there are multiples under the
			// same identifier
			lang := ""
			switch co.SetCode {
			case "FBB", "LEGITA", "DRKITA":
				lang = "Italian"
			case "4BB", "CHRJPN":
				lang = "Japanese"
			}
			ogFields, err = grabLastSold(searchRes.CardId, lang)
			if err != nil {
				errMsg = "Internal bot error ┏༼ ◉ ╭╮ ◉༽┓"
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

func printings2line(printings []string) string {
	line := strings.Join(printings, ", ")
	if len(printings) > MaxPrintings {
		line = strings.Join(printings[:MaxPrintings], ", ") + " and more"
	}
	return line
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

	printings := printings2line(co.Printings)
	if searchRes.EditionSearched != "" && len(co.Variations) > 0 {
		cn := []string{co.Number}
		for _, varid := range co.Variations {
			co, err := mtgmatcher.GetUUID(varid)
			if err != nil {
				continue
			}
			cn = append(cn, co.Number)
		}
		sort.Slice(cn, func(i, j int) bool {
			// Try integer comparison first
			cInum, errI := strconv.Atoi(cn[i])
			cJnum, errJ := strconv.Atoi(cn[j])
			if errI == nil && errJ == nil {
				return cInum < cJnum
			}
			// Else do a string comparison
			return cn[i] < cn[j]
		})
		printings = fmt.Sprintf("%s. Variants in %s are %s", printings, searchRes.EditionSearched, strings.Join(cn, ", "))
	}

	link := "https://www.mtgban.com/search?q=" + co.UUID + "&utm_source=banbot&utm_affiliate=" + guildId

	// Set title of the main message
	title := "Prices for " + card.Name
	if lastSold {
		title = "TCG Last Sold prices for " + card.Name

		tcgId := co.Identifiers["tcgplayerProductId"]
		if co.Etched {
			id, found := co.Identifiers["tcgplayerEtchedProductId"]
			if found {
				tcgId = id
			}
		}

		productId, _ := strconv.Atoi(tcgId)
		printing := "Normal"
		if co.Etched || co.Foil {
			printing = "Foil"
		}
		link = tcgplayer.TCGPlayerProductURL(productId, printing, Config.Affiliate["TCG"])
	}

	// Add a tag for ease of debugging
	if DevMode {
		title = "[DEV] " + title
	}
	// Spark-ly
	if card.Etched {
		title += " 💫"
	} else if card.Foil {
		title += " ✨"
	}

	desc := fmt.Sprintf("[%s] %s\n", card.SetCode, card.Title)
	if !co.Sealed {
		desc = fmt.Sprintf("%sPrinted in %s.\n", desc, printings)
	}
	desc += "\n"

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
func processSellersResults(foundSellers map[string]map[string][]SearchEntry, index bool) (results []SearchEntry) {
	if len(foundSellers) == 0 {
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

	cardId := sortedKeysSeller[0]
	if index {
		results = foundSellers[cardId]["INDEX"]

		// Add the TCG_DIRECT to the Index section too, considering conditions
		for _, cond := range []string{"NM", "SP"} {
			done := false
			foundResults := foundSellers[cardId][cond]
			for _, result := range foundResults {
				if result.ScraperName == TCG_DIRECT {
					results = append(results, result)
					done = true
					break
				}
			}
			if done {
				break
			}
		}
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
func processVendorsResults(foundVendors map[string][]SearchEntry) []SearchEntry {
	if len(foundVendors) == 0 {
		return nil
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

	return foundVendors[sortedKeysVendor[0]]
}

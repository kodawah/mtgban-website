package main

import (
	"errors"
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
	"golang.org/x/exp/slices"

	"github.com/mtgban/go-mtgban/mtgban"
	"github.com/mtgban/go-mtgban/mtgmatcher"
	"github.com/mtgban/go-mtgban/tcgplayer"
)

var poweredByFooter = discordgo.MessageEmbedFooter{
	IconURL: "https://www.mtgban.com/img/logo/ban-round.png",
	Text:    "Powered by mtgban.com",
}

// Scryfall-compatible mode
var squareBracketsRE = regexp.MustCompile(`\[\[.*?\]\]?`)

// Pricefall-only mode
var curlyBracketsRE = regexp.MustCompile(`\{\{.*?\}\}?`)

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

var dg *discordgo.Session

func setupDiscord() error {
	var err error

	if Config.DiscordToken == "" {
		return errors.New("no discord token")
	}

	// Create a new Discord session using the provided bot token.
	dg, err = discordgo.New("Bot " + Config.DiscordToken)
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
}

// Cleanly close down the Discord session.
func cleanupDiscord() {
	if Config.DiscordToken == "" {
		return
	}
	log.Println("Closing connection with Discord")
	dg.Close()
}

// This function will be called every time the bot is invited to a discord
// server and tries to join it.
func guildCreate(s *discordgo.Session, gc *discordgo.GuildCreate) {
	// Set a "is playing" status
	s.UpdateGameStatus(0, "http://mtgban.com")

	// If guild is authorized, then we can proceed as normal
	if slices.Contains(Config.DiscordAllowList, gc.Guild.ID) {
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

	msg := fmt.Sprintf("%s (%s) attempted to install the bot", gc.Guild.Name, gc.Guild.ID)
	UserNotify("bot", msg, true)
	log.Println("unauthorized installation attempt:", msg)
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
	"30A",
	"4BB",
	"CHRJPN",
	"DPA",
	"DRKITA",
	"FBB",
	"LEGITA",
	"O90P",
	"OC13",
	"OC14",
	"OC15",
	"OC16",
	"OC17",
	"OC18",
	"OC19",
	"OC20",
	"OCM1",
	"OCMD",
	"PDP10",
	"PDP12",
	"PDP13",
	"PDP14",
	"PDP15",
	"PDTP",
	"PS14",
	"PS15",
	"PS16",
	"PS17",
	"PS18",
	"PS19",
	"PSDC",
	"PTC",
	"RIN",
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

func parseMessage(content string) (*searchResult, string) {
	// Clean up query, no blocklist because we only need keys
	config := parseSearchOptionsNG(content, nil, nil)
	query := config.CleanQuery

	// Prevent useless invocations
	if len(query) < 3 && query != "Ow" && query != "X" {
		return &searchResult{Invalid: true}, ""
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
				return nil, fmt.Sprintf("No edition found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", editionSearched)
			}
			msg := fmt.Sprintf("No card found named \"%s\" in %s 乁| ･ิ ∧ ･ิ |ㄏ", query, set.Name)
			printings, err := mtgmatcher.Printings4Card(query)
			if err == nil {
				msg = fmt.Sprintf("%s\n\"%s\" is printed in %s.", msg, query, printings2line(printings))
			}
			return nil, msg
		}
		return nil, fmt.Sprintf("No card found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", query)
	}

	if len(uuids) == 0 {
		return nil, fmt.Sprintf("No results found for \"%s\" 乁| ･ิ ∧ ･ิ |ㄏ", query)
	}

	// Keep the first (most recent) result
	sort.Slice(uuids, func(i, j int) bool {
		return sortSets(uuids[i], uuids[j])
	})
	cardId := uuids[0]

	return &searchResult{
		CardId:          cardId,
		EditionSearched: editionSearched,
	}, ""
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

const (
	emoteShurg = "o͡͡͡╮༼ • ʖ̯ • ༽╭o͡͡͡"
	emoteSad   = "┏༼ ◉ ╭╮ ◉༽┓"
	emoteSleep = "(-, – )…zzzZZZ"
	emoteHappy = "ᕕ( ՞ ᗜ ՞ )ᕗ"
)

func grabLastSold(cardId string, lang string) ([]embedField, error) {
	var fields []embedField

	lastSales, err := getLastSold(cardId)
	if err != nil {
		return nil, err
	}

	var hasValues bool
	for _, entry := range lastSales {
		// Skip any language non matching the requested language
		if entry.Language != lang {
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
		return nil, nil
	}

	return fields, nil
}

type AffiliateConfig struct {
	// The text upon which the URL is detected
	Trigger string

	// Skip the identified URL if it contains any of the text
	Skip []string

	// Name of the store (displayed in the title)
	Name string

	// Key to access the Config.Affiliate map
	Handle string

	// List of query parameters to be set to the same config value
	DefaultFields []string

	// Any custom query parameters to be set with the associated value
	CustomFields map[string]string

	// Function to build the displayed title
	TitleFunc func(string) string

	// Whether to parse the entire URL or just its path
	FullURL bool
}

var AffiliateStores []AffiliateConfig = []AffiliateConfig{
	{
		Trigger:       "cardkingdom.com/mtg",
		Name:          "Card Kingdom",
		Handle:        "CK",
		DefaultFields: []string{"partner", "utm_source", "utm_campaign"},
		CustomFields: map[string]string{
			"utm_medium": "affiliate",
		},
	},
	{
		Trigger:       "cardkingdom.com/purchasing",
		Name:          "Card Kingdom",
		Handle:        "CK",
		DefaultFields: []string{"partner", "utm_source", "utm_campaign"},
		CustomFields: map[string]string{
			"utm_medium": "affiliate",
		},
		FullURL: true,
		TitleFunc: func(URL string) string {
			title := "Your search"
			u, err := url.Parse(URL)
			if err != nil {
				return title
			}
			name := u.Query().Get("filter[name]")
			cleanName, err := url.QueryUnescape(name)
			if err != nil {
				return title
			}
			return mtgmatcher.Title(cleanName)
		},
	},
	{
		Trigger:       "coolstuffinc.com/p",
		Name:          "Cool Stuff Inc",
		Handle:        "CSI",
		DefaultFields: []string{"utm_referrer"},
		TitleFunc: func(URLpath string) string {
			base, err := url.QueryUnescape(path.Base(URLpath))
			if err != nil {
				return ""
			}
			return mtgmatcher.Title(base)
		},
	},
	{
		Trigger:       "tcgplayer.com",
		Skip:          []string{"seller", "help"},
		Name:          "TCGplayer",
		Handle:        "TCG",
		DefaultFields: []string{"partner", "utm_source", "utm_medium"},
		CustomFields: map[string]string{
			"utm_campaign": "affliate",
		},
		TitleFunc: func(URLpath string) string {
			var title string
			// The old style links do not have the product id and have an extra element
			if strings.HasSuffix(URLpath, "/listing") {
				title = mtgmatcher.Title(strings.Replace(path.Base(strings.TrimSuffix(URLpath, "/listing")), "-", " ", -1))
			}
			// Sometimes there is the product id embedded in the URL,
			// try to find it and use it to decorate the title
			productId := strings.TrimPrefix(URLpath, "/product/")

			slashIndex := strings.Index(productId, "/")
			if slashIndex != -1 {
				productId = productId[:slashIndex]
			}
			productId = mtgmatcher.Tcg2UUID(productId)
			// Finish is ignored here but it is preserved in the original URL
			co, err := mtgmatcher.GetUUID(productId)
			if err != nil {
				return title
			}
			// Keep the edition first to mimic the normal style
			return fmt.Sprintf("Magic %s %s", co.Edition, co.Name)
		},
	},
	{
		Trigger:       "starcitygames.com/",
		Skip:          []string{"sellyourcards"},
		Name:          "Star City Games",
		Handle:        "SCG",
		DefaultFields: []string{"aff"},
		TitleFunc: func(URLpath string) string {
			urlpath := strings.ToLower(URLpath)
			if strings.Contains(urlpath, "-sgl-") {
				index := strings.Index(urlpath, "-sgl-")
				return mtgmatcher.Title(strings.Replace(urlpath[1:index], "-", " ", -1))
			}
			return "Your search"
		},
	},
	{
		Trigger:       "amazon.com/",
		Skip:          []string{"images"},
		Name:          "Amazon",
		Handle:        "AMZN",
		DefaultFields: []string{"tag"},
		TitleFunc: func(URLpath string) string {
			if strings.Contains(URLpath, "/dp/") {
				fields := strings.Split(URLpath, "/")
				return strings.Replace(fields[2], "-", " ", -1)
			}
			return "Your search"
		},
	},
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore requests if starting up
	if !DatabaseLoaded {
		return
	}

	// Ignore messages coming from unauthorized discords
	if !slices.Contains(Config.DiscordAllowList, m.GuildID) {
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

		switch {
		// Check if selected channels can replace scryfall searches
		case (m.ChannelID == DevChannelID || m.ChannelID == RecapChannelID || m.ChannelID == ChatChannelID) && strings.Contains(m.Content, "[["):
			fields := squareBracketsRE.FindAllString(m.Content, -1)
			for _, field := range fields {
				m.Content = "!" + strings.TrimRight(strings.TrimLeft(field, "["), "]")
				messageCreate(s, m)
			}
		// Check if the message uses the Pricefall syntax
		case strings.Contains(m.Content, "{{"):
			fields := curlyBracketsRE.FindAllString(m.Content, -1)
			for _, field := range fields {
				m.Content = "!" + strings.TrimRight(strings.TrimLeft(field, "{"), "}")
				messageCreate(s, m)
			}
		// Check if we can intercept Gatherer requests
		case strings.Contains(m.Content, "gatherer.wizards.com"):
			fields := strings.Fields(m.Content)
			for _, field := range fields {
				if !strings.Contains(field, "gatherer.wizards.com") {
					continue
				}
				u, err := url.Parse(field)
				if err != nil {
					continue
				}
				mid := u.Query().Get("multiverseid")
				uuids := mtgmatcher.GetUUIDs()
				for _, uuid := range uuids {
					co, _ := mtgmatcher.GetUUID(uuid)
					if co.Identifiers["multiverseId"] == mid {
						m.Content = fmt.Sprintf("!%s|%s|%s", co.Name, co.SetCode, co.Number)
						messageCreate(s, m)
						return
					}
				}
			}
		// Check if the message contains potential links
		default:
			for _, store := range AffiliateStores {
				if !strings.Contains(m.Content, store.Trigger) {
					continue
				}
				shouldSkip := false
				for _, skip := range store.Skip {
					if strings.Contains(m.Content, skip) {
						shouldSkip = true
						break
					}
				}
				if shouldSkip {
					continue
				}

				// Iterate over each segment of the message and look for known links
				fields := strings.Fields(m.Content)
				for _, field := range fields {
					if !strings.Contains(field, store.Trigger) {
						continue
					}
					u, err := url.Parse(field)
					if err != nil {
						continue
					}

					// Extract a sensible link title
					title := mtgmatcher.Title(strings.Replace(path.Base(u.Path), "-", " ", -1))

					var customTitle string
					if store.TitleFunc != nil {
						if store.FullURL {
							customTitle = store.TitleFunc(u.String())
						} else {
							customTitle = store.TitleFunc(u.Path)
						}
						if customTitle != "" {
							title = customTitle
						}
					}
					title += " at " + store.Name

					// Add the MTGBAN affiliation
					v := u.Query()
					for _, value := range store.DefaultFields {
						v.Set(value, Config.Affiliate[store.Handle])
					}
					for storeField, value := range store.CustomFields {
						v.Set(storeField, value)
					}
					u.RawQuery = v.Encode()

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
		}
		return
	}

	allBls := strings.HasPrefix(m.Content, "!")
	lastSold := strings.HasPrefix(m.Content, "$$")

	// Strip away beginning character
	content := strings.TrimPrefix(m.Content, "!")
	content = strings.TrimPrefix(content, "$$")

	// Search a single card match
	searchRes, errMsg := parseMessage(content)
	if errMsg != "" {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Description: errMsg,
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

		// Skip non-NM buylist prices
		config.EntryFilters = append(config.EntryFilters, FilterEntryElem{
			Name:          "condition",
			Values:        []string{"NM"},
			OnlyForVendor: true,
		})

		cardIds, _ := searchAndFilter(config)
		foundSellers, foundVendors := searchParallelNG(cardIds, config)

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
			ogFields, err = grabLastSold(searchRes.CardId, co.Language)
			if err != nil {
				if errors.Is(err, ErrMissingTCGId) {
					errMsg = fmt.Sprintf("\"%s\" does not have any identifier set, I don't know what to do %s", content, emoteShurg)
				} else {
					errMsg = "Internal bot error " + emoteSad
					log.Println("Bot error:", err, "from", content)
				}
			} else if len(ogFields) == 0 {
				errMsg = fmt.Sprintf("No Last Sold Price available for \"%s\" %s", content, emoteShurg)
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
		embed.Description += "Grabbing last sold prices, hang tight " + emoteHappy
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
			edit.Description += "Connection time out " + emoteSleep
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
		link = tcgplayer.TCGPlayerProductURL(productId, printing, Config.Affiliate["TCG"], co.Language)
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
		for _, cond := range mtgban.DefaultGradeTags {
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
func processVendorsResults(foundVendors map[string]map[string][]SearchEntry) []SearchEntry {
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

	return foundVendors[sortedKeysVendor[0]]["NM"]
}

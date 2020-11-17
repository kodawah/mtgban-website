package main

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

var poweredByFooter = discordgo.MessageEmbedFooter{
	IconURL: "https://www.mtgban.com/img/logo/ban-round.png",
	Text:    "Powered by mtgban.com",
}

const (
	// Avoid making messages overly long
	MaxPrintings = 12

	// Overflow prevention for field.Value size
	MaxCustomEntries = 7

	// Discord API constants
	MaxEmbedFieldsValueLength = 1024
	MaxEmbedFieldsNumber      = 25
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
	// If guild is authorized, then we can proceed as normal
	if stringSliceContains(Config.DiscordAllowList, gc.Guild.ID) {
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

	// Avoid invocations
	if strings.HasPrefix(m.Content, "!") {
		// Strip away bang character
		content := strings.TrimPrefix(m.Content, "!")

		// Clean up query and only search for NM
		query, options := parseSearchOptions(content)

		// Set a custom search mode since we want to try and find as much as possible
		if options["search_mode"] == "" {
			options["search_mode"] = "any"
		}

		// Prevent useless invocations
		if len(query) < 3 && query != "Ow" && query != "X" {
			return
		}

		// Check if card exists
		nameFound := false
		sets := mtgmatcher.GetSets()
		if options["edition"] != "" {
			set, found := sets[options["edition"]]
			if !found {
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Description: "No card found named \"" + query + "\" in \"" + options["edition"] + "\" 乁| ･ิ ∧ ･ิ |ㄏ",
				})
				return
			}
			for _, card := range set.Cards {
				if mtgmatcher.Contains(card.Name, query) {
					nameFound = true
					break
				}
			}
			if !nameFound {
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Description: "No card found named \"" + query + "\" in " + set.Name + " 乁| ･ิ ∧ ･ิ |ㄏ",
				})
				return
			}
		}
		if !nameFound {
			for _, set := range sets {
				for _, card := range set.Cards {
					if mtgmatcher.Contains(card.Name, query) {
						nameFound = true
						break
					}
				}
				if nameFound {
					break
				}
			}
		}
		if !nameFound {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Description: "No card found for \"" + query + "\" 乁| ･ิ ∧ ･ิ |ㄏ",
			})
			return
		}

		// Search both sellers and vendors
		cardId, resultsSellers, errS := searchSellersFirstResult(query, options)
		cardIdV, resultsVendors, errV := searchVendorsFirstResult(query, options)
		switch {
		// Both errored, card is oos
		case errS != nil && errV != nil:
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Description: errS.Error(),
			})
			return
		// Retail is oos, but buylist isn't, let's use it
		case errS != nil:
			cardId = cardIdV
		// Buylist is not oos, but it returned a different card id,
		// which means the original one is actually oos
		case errV == nil && cardId != cardIdV:
			resultsVendors = []SearchEntry{}
		}

		// Add two embed filds, one for retail and one for buylist
		fieldsNames := []string{"Retail", "Buylist"}
		var fields []*discordgo.MessageEmbedField
		for i, results := range [][]SearchEntry{resultsSellers, resultsVendors} {
			field := &discordgo.MessageEmbedField{
				Name:   fieldsNames[i],
				Inline: true,
			}

			// Results look really bad after MaxCustomEntries, and too much info
			// does not help, so sort by best price, trim, then sort back to original
			if len(results) > MaxCustomEntries {
				if i == 0 {
					sort.Slice(results, func(i, j int) bool {
						return results[i].Price < results[j].Price
					})
				} else if i == 1 {
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
				link := "https://" + DefaultHost + "/" + path.Join("go", kind, store, cardId)

				// Set the custom field
				value := fmt.Sprintf("• **[`%s%s`](%s)** $%0.2f", entry.ScraperName, extraSpaces, link, entry.Price)
				if entry.Ratio > 60 {
					value += fmt.Sprintf(" 🔥")
				}
				value += "\n"

				// If we go past the maximum value for embed field values,
				// make a new field for any spillover, as long as we are within
				// the limits of the number of embeds allowed
				if len(field.Value)+len(value) > MaxEmbedFieldsValueLength && len(fields) < MaxEmbedFieldsNumber {
					fields = append(fields, field)
					field = &discordgo.MessageEmbedField{
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

		// Prepare card data
		card := uuid2card(cardId, true)

		// Retrieve the first 12 editions this card is printed in
		printings := "several sets"
		co, err := mtgmatcher.GetUUID(cardId)
		if err == nil {
			printings = strings.Join(co.Printings, ", ")
			if len(co.Printings) > MaxPrintings {
				printings = strings.Join(co.Printings[:MaxPrintings], ", ") + " and more"
			}
			if options["edition"] != "" && len(co.Variations) > 0 {
				cn := []string{co.Number}
				for _, varid := range co.Variations {
					co, err := mtgmatcher.GetUUID(varid)
					if err != nil {
						continue
					}
					cn = append(cn, co.Number)
				}
				printings = fmt.Sprintf("%s. Variants in %s are %s", printings, options["edition"], strings.Join(cn, ", "))
			}
		}

		var link string
		// Rebuild the search query
		searchQuery := card.Name
		if options["edition"] != "" {
			searchQuery += " s:" + options["edition"]
		}
		if options["number"] != "" {
			searchQuery += " cn:" + options["number"]
		}
		if options["foil"] != "" {
			searchQuery += " f:" + options["foil"]
		}
		link = "https://www.mtgban.com/search?q=" + url.QueryEscape(searchQuery) + "&utm_source=banbot&utm_affiliate=" + m.GuildID

		// Set title of the main message
		title := "Prices for " + card.Name
		// Add a tag for ease of debugging
		if DevMode {
			title = "[DEV] " + title
		}
		// Spark-ly
		if card.Foil {
			title += " ✨"
		}

		embed := discordgo.MessageEmbed{
			Title:       title,
			Color:       0xFF0000,
			URL:         link,
			Description: fmt.Sprintf("[%s] %s\nPrinted in %s", card.SetCode, card.Title, printings),
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
		_, stocks := Infos["STKS"][cardId]
		if stocks {
			embed.Footer.Text += "On MTGStocks Interests page\n"
		}
		// Show data source on non-ban servers
		if len(Config.DiscordAllowList) > 0 && m.GuildID != Config.DiscordAllowList[0] {
			embed.Footer.IconURL = poweredByFooter.IconURL
			embed.Footer.Text += poweredByFooter.Text
		}

		s.ChannelMessageSendEmbed(m.ChannelID, &embed)
	}
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
func searchSellersFirstResult(query string, options map[string]string) (cardId string, results []SearchEntry, err error) {
	options["condition"] = "NM"

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
	results = foundSellers[cardId]["NM"]

	// Drop duplicates by looking at the last one as they are alredy sorted
	tmp := append(results[:0], results[0])
	for i := range results {
		if results[i].ScraperName != tmp[len(tmp)-1].ScraperName {
			tmp = append(tmp, results[i])
		}
	}
	results = tmp
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

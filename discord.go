package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/kodabb/go-mtgban/mtgmatcher"
)

func setupDiscord() error {
	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + Config.DiscordToken)
	if err != nil {
		return err
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// In this example, we only care about receiving message events.
	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		return err
	}

	return nil
	// Cleanly close down the Discord session.
	//dg.Close()
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by a bot
	if m.Author.Bot {
		return
	}

	wantSellers := strings.HasPrefix(m.Content, "!")
	wantVendors := strings.HasPrefix(m.Content, "?")
	if wantSellers || wantVendors {
		// Strip away bang character
		content := strings.TrimPrefix(m.Content, "!")
		content = strings.TrimPrefix(content, "?")

		// Clean up query and only search for NM
		query, options := parseSearchOptions(content)

		// Check if card exists
		printings, err := mtgmatcher.GetPrintings(query)
		if err != nil {
			if options["search_mode"] == "" || options["search_mode"] == "exact" {
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Description: "No card found for \"" + query + "\" 乁| ･ิ ∧ ･ิ |ㄏ",
				})
				return
			}
		}

		var cardId string
		var results []SearchEntry
		if wantSellers {
			options["condition"] = "NM"

			// Search
			foundSellers, _ := searchSellers(query, Config.SearchBlockList, options)
			if len(foundSellers) == 0 {
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Description: "Out of stock everywhere ┻━┻ ヘ╰( •̀ε•́ ╰)",
				})
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
		} else if wantVendors {
			// Search
			foundVendors, _ := searchVendors(query, Config.SearchBlockList, options)
			if len(foundVendors) == 0 {
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Description: "Nobody is buying that card ┻━┻ ヘ╰( •̀ε•́ ╰)",
				})
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
		}

		// Results are limited to 10 by API, sort by best price, trim,
		// then sort the array back to original
		if len(results) > 10 {
			if wantSellers {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price < results[j].Price
				})
			} else if wantSellers {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price > results[j].Price
				})
			}
			results = results[:10]
			sort.Slice(results, func(i, j int) bool {
				return results[i].ScraperName < results[j].ScraperName
			})
		}

		var fields []*discordgo.MessageEmbedField
		for _, entry := range results {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   entry.ScraperName,
				Value:  fmt.Sprintf("$ %0.2f", entry.Price),
				Inline: true,
			})
		}

		card := uuid2card(cardId, true)

		// Just in case the original array didn't survive
		co, err := mtgmatcher.GetUUID(cardId)
		if err == nil {
			printings = co.Printings
		}

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

		var title string
		if wantSellers {
			title = "Retail prices for " + card.Name
		} else if wantVendors {
			title = "Buylist prices for " + card.Name
		}

		embed := discordgo.MessageEmbed{
			Title: title,
			Color: 0xFF0000,
			URL:   "https://www.mtgban.com/search?q=" + url.QueryEscape(searchQuery),
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: card.ImageURL,
			},
		}

		if card.Foil {
			embed.Title += " ✨"
		}
		embed.Description = fmt.Sprintf("[%s] %s\nPrinted in %s", card.SetCode, card.Title, strings.Join(printings, ", "))

		embed.Fields = fields

		if card.Reserved {
			embed.Footer = &discordgo.MessageEmbedFooter{
				Text: "Part of the Reserved List",
			}
		}

		s.ChannelMessageSendEmbed(m.ChannelID, &embed)
	}
}

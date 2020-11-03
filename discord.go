package main

import (
	"errors"
	"fmt"
	"net/url"
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
	// As defined by the discord API
	MaxEmbeds = 10

	// Avoid making messages overly long
	MaxPrintings = 12
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

	wantSellers := strings.HasPrefix(m.Content, "!")
	wantVendors := strings.HasPrefix(m.Content, "?")
	// Avoid invocations
	if wantSellers || wantVendors {
		// Strip away bang character
		content := strings.TrimPrefix(m.Content, "!")
		content = strings.TrimPrefix(content, "?")
		content = strings.TrimPrefix(content, "$")

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

		var err error
		var ogScraperName string
		var cardId string
		var results []SearchEntry
		if wantSellers {
			cardId, results, err = searchSellersFirstResult(query, options)
		} else if wantVendors {
			cardId, results, err = searchVendorsFirstResult(query, options)
		}
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Description: err.Error(),
			})
			return
		}

		// Results are limited to 10 by API, sort by best price, trim,
		// then sort the array back to original
		if len(results) > MaxEmbeds {
			if wantSellers {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price < results[j].Price
				})
			} else if wantSellers {
				sort.Slice(results, func(i, j int) bool {
					return results[i].Price > results[j].Price
				})
			}
			results = results[:MaxEmbeds]
			sort.Slice(results, func(i, j int) bool {
				return results[i].ScraperName < results[j].ScraperName
			})
		}

		var fields []*discordgo.MessageEmbedField
		for _, entry := range results {
			price := "N/A"
			if entry.ScraperName == "Ratio" {
				price = fmt.Sprintf("%0.2f %%", entry.Price)
			} else if entry.Price > 0 {
				price = fmt.Sprintf("$ %0.2f", entry.Price)
				// Also add quantity for hybrid mode
				if entry.Quantity > 0 && (entry.ScraperName == "Retail" || entry.ScraperName == "Buylist") {
					price = fmt.Sprintf("%d @ %s", entry.Quantity, price)
				}
			}
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   entry.ScraperName,
				Value:  price,
				Inline: true,
			})
		}

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

		var title string
		if wantSellers {
			title = "Retail prices for " + card.Name
		} else if wantVendors {
			title = "Buylist prices for " + card.Name
		}

		// Add a tag for ease of debugging
		if DevMode {
			title = "[DEV] " + title
		}

		embed := discordgo.MessageEmbed{
			Title: title,
			Color: 0xFF0000,
			URL:   link,
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: card.ImageURL,
			},
		}

		if card.Foil {
			embed.Title += " ✨"
		}
		embed.Description = fmt.Sprintf("[%s] %s\nPrinted in %s", card.SetCode, card.Title, printings)

		embed.Fields = fields

		if card.Reserved {
			embed.Footer = &discordgo.MessageEmbedFooter{
				Text: "Part of the Reserved List",
			}
		}

		// Show data source on non-ban servers
		if len(Config.DiscordAllowList) > 0 && m.GuildID != Config.DiscordAllowList[0] {
			embed.Footer = &poweredByFooter
		}

		s.ChannelMessageSendEmbed(m.ChannelID, &embed)
	}
}

// Retrieve cards from Sellers using the very first result
func searchSellersFirstResult(query string, options map[string]string) (cardId string, results []SearchEntry, err error) {
	options["condition"] = "NM"

	// Search
	foundSellers, _ := searchSellers(query, Config.SearchBlockList, options)
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

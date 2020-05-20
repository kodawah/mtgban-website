package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/abugames"
	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/channelfireball"
	"github.com/kodabb/go-mtgban/miniaturemarket"
	"github.com/kodabb/go-mtgban/ninetyfive"
	"github.com/kodabb/go-mtgban/strikezone"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
)

type NavElem struct {
	Active bool
	Class  string
	Link   string
	Name   string
}

type Arbitrage struct {
	Name       string
	LastUpdate string
	Arbit      []mtgban.ArbitEntry
	Len        int
}

type PageVars struct {
	Nav       []NavElem
	Signature string
	Expires   string

	Title        string
	CKPartner    string
	ErrorMessage string
	InfoMessage  string
	LastUpdate   string

	SearchQuery  string
	CondKeys     []string
	FoundSellers map[mtgdb.Card]map[string][]mtgban.CombineEntry
	FoundVendors map[mtgdb.Card][]mtgban.CombineEntry
	Images       map[mtgdb.Card]string

	SellerShort  string
	SellerFull   string
	SellerUpdate string

	Arb       []Arbitrage
	UseCredit bool
}

var DefaultNav = []NavElem{
	NavElem{
		Name: "Home",
		Link: "/",
	},
	NavElem{
		Name: "Search",
		Link: "/search",
	},
	NavElem{
		Name: "Arbitrage",
		Link: "arbit",
	},
}

var BanClient *mtgban.BanClient
var DevMode bool
var CKPartner string
var DatabaseLoaded bool
var LastUpdate time.Time
var Sellers []mtgban.Seller
var Vendors []mtgban.Vendor

func Favicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "img/misc/favicon.ico")
}

// FileSystem custom file system handler
type FileSystem struct {
	httpfs http.FileSystem
}

// Open opens file
func (fs *FileSystem) Open(path string) (http.File, error) {
	f, err := fs.httpfs.Open(path)
	if err != nil {
		return nil, err
	}

	s, err := f.Stat()
	if s.IsDir() {
		index := strings.TrimSuffix(path, "/") + "/index.html"
		_, err := fs.httpfs.Open(index)
		if err != nil {
			return nil, err
		}
	}

	return f, nil
}

func periodicFunction() {
	log.Println("Updating data")

	newbc := mtgban.NewClient()

	newck := cardkingdom.NewScraper()
	newck.Partner = CKPartner
	newck.LogCallback = log.Printf

	newsz := strikezone.NewScraper()
	newsz.LogCallback = log.Printf

	newabu := abugames.NewScraper()
	newabu.LogCallback = log.Printf

	newcfb := channelfireball.NewScraper()
	newcfb.LogCallback = log.Printf

	newmm := miniaturemarket.NewScraper()
	newmm.LogCallback = log.Printf

	new95 := ninetyfive.NewScraper()
	new95.LogCallback = log.Printf

	newbc.Register(newck)
	newbc.Register(newsz)
	newbc.Register(new95)
	if !DevMode {
		newbc.Register(newabu)
		newbc.Register(newcfb)
		newbc.Register(newmm)
	}

	// Load inventory first and then buylists
	// Return as much memory as possible between runs to prevent running out
	// of memory quota on heroku
	newSellers := newbc.Sellers()
	sort.Slice(newSellers, func(i, j int) bool {
		return strings.Compare(newSellers[i].Info().Name, newSellers[j].Info().Name) < 0
	})
	for _, seller := range newSellers {
		_, err := seller.Inventory()
		debug.FreeOSMemory()
		log.Println(seller.Info().Name)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("-- OK")
	}

	newVendors := newbc.Vendors()
	sort.Slice(newVendors, func(i, j int) bool {
		return strings.Compare(newVendors[i].Info().Name, newVendors[j].Info().Name) < 0
	})
	for _, vendor := range newVendors {
		_, err := vendor.Buylist()
		debug.FreeOSMemory()
		log.Println(vendor.Info().Name)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("-- OK")
	}

	BanClient = newbc
	Sellers = newSellers
	Vendors = newVendors

	LastUpdate = time.Now()

	log.Println("Scrapers loaded")
}

func loadDB() error {
	respPrintings, err := http.Get("https://www.mtgjson.com/files/AllPrintings.json")
	if err != nil {
		return err
	}
	defer respPrintings.Body.Close()

	respCards, err := http.Get("https://www.mtgjson.com/files/AllCards.json")
	if err != nil {
		return err
	}
	defer respCards.Body.Close()

	return mtgdb.RegisterWithReaders(respPrintings.Body, respCards.Body)
}

func main() {
	devMode := flag.Bool("dev", false, "Enable developer mode")
	flag.Parse()
	DevMode = *devMode

	// load website up
	go func() {
		var err error

		log.Println("Loading MTGJSON")
		if DevMode {
			err = mtgdb.RegisterWithPaths("allprintings.json", "allcards.json")
		} else {
			err = loadDB()
		}
		if err != nil {
			log.Fatalln(err)
		}

		periodicFunction()
		DatabaseLoaded = true
	}()

	// load necessary environmental variables
	CKPartner = os.Getenv("CARDKINGDOM_PARTNER")
	if CKPartner == "" {
		log.Fatalln("CARDKINGDOM_PARTNER not set")
	}
	dataRefresh := os.Getenv("DATA_REFRESH")
	refresh, _ := strconv.Atoi(dataRefresh)
	if refresh == 0 {
		log.Fatalln("DATA_REFRESH not set")
	}
	if os.Getenv("BAN_SECRET") == "" {
		log.Fatalln("BAN_SECRET not set")
	}

	// refresh every few hours
	go func() {
		for _ = range time.NewTicker(time.Duration(refresh) * time.Hour).C {
			periodicFunction()
		}
	}()

	// serve everything in the css and img folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))

	// when navigating to /home it should serve the home page
	http.HandleFunc("/", Home)
	http.HandleFunc("/search", Search)
	http.HandleFunc("/arbit", Arbit)
	http.HandleFunc("/favicon.ico", Favicon)
	http.ListenAndServe(getPort(), nil)
}

// Detect $PORT and if present uses it for listen and serve else defaults to :8080
// This is so that app can run on Heroku
func getPort() string {
	p := os.Getenv("PORT")
	if p != "" {
		return ":" + p
	}
	return ":8080"
}

func render(w http.ResponseWriter, tmpl string, pageVars PageVars) {
	tmpl = fmt.Sprintf("templates/%s", tmpl) // prefix the name passed in with templates/

	t, err := template.ParseFiles(tmpl) // parse the template file held in the templates folder
	if err != nil {                     // if there is an error
		log.Print("template parsing error: ", err) // log it
	}

	err = t.Execute(w, pageVars) // execute the template and pass in the variables to fill the gaps
	if err != nil {              // if there is an error
		log.Print("template executing error: ", err) //log it
	}
}

package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kodabb/go-mtgban/abugames"
	"github.com/kodabb/go-mtgban/cardkingdom"
	"github.com/kodabb/go-mtgban/channelfireball"
	"github.com/kodabb/go-mtgban/miniaturemarket"
	"github.com/kodabb/go-mtgban/strikezone"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgjson"
)

var ck *cardkingdom.Cardkingdom
var sz *strikezone.Strikezone
var abu *abugames.ABUGames
var cfb *channelfireball.Channelfireball
var mm *miniaturemarket.Miniaturemarket

type PageVars struct {
	Title      string
	CKPartner  string
	Message    string
	LastUpdate string

	SellerShort string
	SellerFull  string
	VendorShort string
	VendorFull  string

	Arb []mtgban.ArbitEntry
}

var CKPartner string
var DB mtgjson.MTGDB
var LastUpdate time.Time

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

func periodicFunction(t time.Time, db mtgjson.MTGDB) {
	log.Println("Updating data")

	newck := cardkingdom.NewScraper(db)
	newck.Partner = CKPartner
	newck.LogCallback = log.Printf
	_, err := newck.Buylist()
	if err != nil {
		log.Println(err)
		return
	}
	newsz := strikezone.NewScraper(db)
	newsz.LogCallback = log.Printf
	_, err = newsz.Buylist()
	if err != nil {
		log.Println(err)
		return
	}
	newabu := abugames.NewScraper(db)
	newabu.LogCallback = log.Printf
	_, err = newabu.Buylist()
	if err != nil {
		log.Println(err)
		return
	}
	newcfb := channelfireball.NewScraper(db)
	newcfb.LogCallback = log.Printf
	_, err = newcfb.Buylist()
	if err != nil {
		log.Println(err)
		return
	}
	newmm := miniaturemarket.NewScraper(db)
	newmm.LogCallback = log.Printf
	_, err = newmm.Buylist()
	if err != nil {
		log.Println(err)
		return
	}

	ck, sz, abu, cfb, mm = newck, newsz, newabu, newcfb, newmm
	LastUpdate = t

	log.Println("DONE")
}

func main() {
	// load website up
	go func() {
		log.Println("Loading MTGJSON")
		resp, err := http.Get("https://www.mtgjson.com/files/AllPrintings.json")
		if err != nil {
			log.Fatalln(err)
		}
		defer resp.Body.Close()

		// Load static data once
		db, err := mtgjson.LoadAllPrintingsFromReader(resp.Body)
		if err != nil {
			log.Fatalln(err)
		}

		periodicFunction(time.Now(), db)
		DB = db
	}()

	// refresh every 6 hours
	go func() {
		for t := range time.NewTicker(6 * time.Hour).C {
			periodicFunction(t, DB)
		}
	}()

	// load necessary environmental variables
	CKPartner = os.Getenv("CARDKINGDOM_PARTNER")
	if CKPartner == "" {
		log.Fatalln("CARDKINGDOM_PARTNER not set")
	}

	// serve everything in the css and img folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))

	// when navigating to /home it should serve the home page
	http.HandleFunc("/", Home)
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

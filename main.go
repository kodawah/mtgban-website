package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

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

	PatreonId    string
	PatreonURL   string
	PatreonLogin bool
	ShowPromo    bool

	Title        string
	CKPartner    string
	TCGAffiliate string
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

type TCGArgs struct {
	Affiliate string
	PublicId  string
	PrivateId string
}

var BanClient *mtgban.BanClient
var DevMode bool
var SigCheck bool
var CKPartner string
var TCGConfig TCGArgs
var DatabaseLoaded bool
var LastUpdate time.Time
var Sellers []mtgban.Seller
var Vendors []mtgban.Vendor
var DefaultSellers string
var AdminIds []string

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

func genPageNav(activeTab, sig string) PageVars {
	exp, _ := GetParamFromSig(sig, "Expires")
	expires, _ := strconv.ParseInt(exp, 10, 64)
	msg := ""
	patreonLogin := false
	if expires < time.Now().Unix() {
		if sig != "" {
			msg = ErrMsgExpired
			patreonLogin = true
		}
	}

	pageVars := PageVars{
		Title:        "BAN " + activeTab,
		Signature:    sig,
		PatreonId:    PatreonClientId,
		PatreonURL:   PatreonHost,
		PatreonLogin: patreonLogin,
		ErrorMessage: msg,
		LastUpdate:   LastUpdate.Format(time.RFC3339),
	}
	pageVars.Nav = make([]NavElem, len(DefaultNav))
	copy(pageVars.Nav, DefaultNav)

	signature := ""
	if sig != "" {
		signature = "?sig=" + sig
	}

	mainNavIndex := 0
	for i := range pageVars.Nav {
		pageVars.Nav[i].Link += signature
		if pageVars.Nav[i].Name == activeTab {
			mainNavIndex = i
		}
	}
	pageVars.Nav[mainNavIndex].Active = true
	pageVars.Nav[mainNavIndex].Class = "active"
	return pageVars
}

func main() {
	devMode := flag.Bool("dev", false, "Enable developer mode")
	sigCheck := flag.Bool("sig", false, "Enable signature verification")
	flag.Parse()
	DevMode = *devMode
	SigCheck = true
	if DevMode {
		SigCheck = *sigCheck
	}

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
	TCGConfig = TCGArgs{
		Affiliate: os.Getenv("TCG_AFFILIATE"),
		PublicId:  os.Getenv("TCG_PUBLIC_ID"),
		PrivateId: os.Getenv("TCG_PRIVATE_ID"),
	}
	if TCGConfig.Affiliate == "" || TCGConfig.PublicId == "" || TCGConfig.PrivateId == "" {
		log.Fatalln("TCG configuration not set")
	}
	if os.Getenv("PATREON_SECRET") == "" {
		log.Fatalln("PATREON_SECRET not set")
	}
	DefaultSellers = os.Getenv("DATA_ENABLED")
	if DefaultSellers == "" {
		log.Fatalln("DATA_ENABLED not set")
	}
	ids := os.Getenv("BAN_ADMIN_IDS")
	if ids == "" {
		log.Fatalln("BAN_ADMIN_IDS not set")
	}
	AdminIds = strings.Split(ids, ",")

	// refresh every few hours
	go func() {
		for _ = range time.NewTicker(time.Duration(refresh) * time.Hour).C {
			periodicFunction()
		}
	}()

	// serve everything in known folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))
	http.Handle("/js/", http.StripPrefix("/js/", http.FileServer(&FileSystem{http.Dir("js")})))

	// when navigating to /home it should serve the home page
	http.Handle("/", noSigning(http.HandlerFunc(Home)))
	http.Handle("/search", enforceSigning(http.HandlerFunc(Search)))
	http.Handle("/arbit", enforceSigning(http.HandlerFunc(Arbit)))
	http.HandleFunc("/favicon.ico", Favicon)
	http.HandleFunc("/auth", Auth)
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

	// Clean as much as possible to that we stay under quota
	go debug.FreeOSMemory()
}

package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	cron "gopkg.in/robfig/cron.v2"

	"github.com/kodabb/go-mtgban/mtgban"
	"github.com/kodabb/go-mtgban/mtgdb"
)

type NavElem struct {
	Active bool
	Class  string
	Link   string
	Name   string
	Short  string
}

type Arbitrage struct {
	Name       string
	LastUpdate string
	Arbit      []mtgban.ArbitEntry
	Len        int
	HasCredit  bool
}

type PageVars struct {
	Nav       []NavElem
	Signature string

	PatreonId    string
	PatreonURL   string
	PatreonLogin bool
	ShowPromo    bool

	PatreonPartnerId string

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

	Arb        []Arbitrage
	UseCredit  bool
	FilterCond bool
	FilterFoil bool
	FilterComm bool
}

var DefaultNav = []NavElem{
	NavElem{
		Name:  "🏡 Home",
		Short: "🏡",
		Link:  "/",
	},
	NavElem{
		Name:  "🔍 Search",
		Short: "🔍",
		Link:  "/search",
	},
	NavElem{
		Name:  "📈 Arbitrage",
		Short: "📈",
		Link:  "arbit",
	},
}

type TCGArgs struct {
	Affiliate string
	PublicId  string
	PrivateId string
}

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
var PartnerIds []string
var RootId string
var Refresh int

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

		PatreonPartnerId: PatreonPartnerId,
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
		// Ingore the starting emoji
		if strings.HasSuffix(pageVars.Nav[i].Name, activeTab) {
			mainNavIndex = i
		}
	}
	pageVars.Nav[mainNavIndex].Active = true
	pageVars.Nav[mainNavIndex].Class = "active"
	return pageVars
}

func loadVars() (err error) {
	envVars := map[string]string{}

	keyVars := []string{
		"CARDKINGDOM_PARTNER",
		"DATA_REFRESH",
		"BAN_SECRET",
		"TCG_AFFILIATE",
		"TCG_PUBLIC_ID",
		"TCG_PRIVATE_ID",
		"PATREON_SECRET",
		"PATREON_PARTNER_SECRET",
		"DATA_ENABLED",
		"BAN_ADMIN_IDS",
		"BAN_PARTNER_IDS",
		"BAN_ROOT_ID",
	}
	for _, key := range keyVars {
		v := os.Getenv(key)
		if v == "" {
			return fmt.Errorf("%s variable not set", key)
		}
		envVars[key] = v
	}

	CKPartner = envVars["CARDKINGDOM_PARTNER"]
	Refresh, err = strconv.Atoi(envVars["DATA_REFRESH"])
	if err != nil {
		return err
	}
	TCGConfig = TCGArgs{
		Affiliate: envVars["TCG_AFFILIATE"],
		PublicId:  envVars["TCG_PUBLIC_ID"],
		PrivateId: envVars["TCG_PRIVATE_ID"],
	}
	DefaultSellers = envVars["DATA_ENABLED"]
	AdminIds = strings.Split(envVars["BAN_ADMIN_IDS"], ",")
	PartnerIds = strings.Split(envVars["BAN_PARTNER_IDS"], ",")
	RootId = envVars["BAN_ROOT_ID"]

	return nil
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

	// load necessary environmental variables
	err := loadVars()
	if err != nil {
		log.Fatalln(err)
	}

	// load website up
	go func() {
		var err error

		if !DevMode {
			log.Println("Loading MTGJSONv5")
			err = loadDatastore()
			if err != nil {
				log.Fatalln(err)
			}
		}

		log.Println("Loading MTGJSON")
		err = loadDB()
		if err != nil {
			log.Fatalln(err)
		}

		loadScrapers()
		DatabaseLoaded = true

		// If today's cache is missing, schedule a refresh right away
		fi, err := os.Stat(fmt.Sprintf("cache_inv/%03d", time.Now().YearDay()))
		if os.IsNotExist(err) || !fi.IsDir() {
			log.Println("Loaded too old data, refreshing in the background")
			loadScrapers()
		}
	}()

	c := cron.New()
	// refresh every few hours
	c.AddFunc(fmt.Sprintf("@every %dh", Refresh), loadScrapers)
	// refresh at 12:00 every Tuesday
	c.AddFunc("0 12 * * 2", func() {
		log.Println("Reloading MTGJSON")
		err := loadDB()
		if err != nil {
			log.Println(err)
		}
	})
	c.Start()

	// serve everything in known folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))
	http.Handle("/js/", http.StripPrefix("/js/", http.FileServer(&FileSystem{http.Dir("js")})))

	// when navigating to /home it should serve the home page
	http.Handle("/", noSigning(http.HandlerFunc(Home)))
	http.Handle("/search", enforceSigning(http.HandlerFunc(Search)))
	http.Handle("/arbit", enforceSigning(http.HandlerFunc(Arbit)))
	http.Handle("/api/mtgjson/ck.json", enforceSigning(http.HandlerFunc(API)))
	http.HandleFunc("/favicon.ico", Favicon)
	http.HandleFunc("/auth", Auth)
	log.Fatal(http.ListenAndServe(getPort(), nil))
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
		return
	}

	err = t.Execute(w, pageVars) // execute the template and pass in the variables to fill the gaps
	if err != nil {              // if there is an error
		log.Print("template executing error: ", err) //log it
	}
}

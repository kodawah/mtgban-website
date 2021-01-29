package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"database/sql"

	_ "github.com/go-sql-driver/mysql"
	cron "gopkg.in/robfig/cron.v2"

	"github.com/kodabb/go-mtgban/mtgban"
)

type NavElem struct {
	Active bool
	Class  string
	Link   string
	Name   string
	Short  string
	Handle func(w http.ResponseWriter, r *http.Request)
}

type GenericCard struct {
	Name      string
	Edition   string
	SetCode   string
	Number    string
	Variant   string
	Keyrune   string
	ImageURL  string
	Foil      bool
	Reserved  bool
	Title     string
	SearchURL string
	Stocks    bool
}

type PageVars struct {
	Nav []NavElem

	PatreonId    string
	PatreonURL   string
	PatreonLogin bool
	ShowPromo    bool

	Title        string
	ErrorMessage string
	InfoMessage  string
	LastUpdate   string

	SellerKeys   []string
	VendorKeys   []string
	SearchQuery  string
	SearchBest   bool
	CondKeys     []string
	FoundSellers map[string]map[string][]SearchEntry
	FoundVendors map[string][]SearchEntry
	Metadata     map[string]GenericCard

	SellerShort       string
	SellerFull        string
	SellerUpdate      string
	SellerAffiliate   bool
	SellerNoAvailable bool

	Arb            []Arbitrage
	UseCredit      bool
	FilterCond     bool
	FilterFoil     bool
	FilterComm     bool
	FilterNega     bool
	FilterPenny    bool
	FilterSpread   bool
	FilterQuantity bool
	SortOption     string
	GlobalMode     bool

	Page         string
	ToC          []NewspaperPage
	Headings     []Heading
	Cards        []GenericCard
	Table        [][]string
	HasReserved  bool
	IsOneDay     bool
	CanSwitchDay bool
	TotalIndex   int
	CurrentIndex int
	PrevIndex    int
	NextIndex    int
	SortDir      string
	LargeTable   bool
	OffsetCards  int
	FilterSet    string
	Editions     []string
	FilterRarity string
	Rarities     []string

	Sleepers [7]SleeperEntry

	HasStocks bool
}

var DefaultNav = []NavElem{
	NavElem{
		Name:  "🏡 Home",
		Short: "🏡",
		Link:  "/",
	},
}

var OptionalFields = []string{
	"SearchDisabled",
	"ArbitEnabled",
	"ArbitDisabledVendors",
	"ExpEnabled",
	"NewsEnabled",
	"AnyEnabled",
	"API",
}

var OrderNav = []string{
	"Search",
	"Newspaper",
	"Explore",
	"Sleepers",
	"Global",
	"Arbit",
}

var ExtraNavs map[string]NavElem

func init() {
	ExtraNavs = map[string]NavElem{
		"Search": NavElem{
			Name:   "🔍 Search",
			Short:  "🔍",
			Link:   "/search",
			Handle: Search,
		},
		"Newspaper": NavElem{
			Name:   "🗞️ Newspaper",
			Short:  "🗞️",
			Link:   "/newspaper",
			Handle: Newspaper,
		},
		"Explore": NavElem{
			Name:   "🚠 Explore",
			Short:  "🚠",
			Link:   "/explore",
			Handle: Explore,
		},
		"Sleepers": NavElem{
			Name:   "💤 Sleepers",
			Short:  "💤",
			Link:   "/sleepers",
			Handle: Sleepers,
		},
		"Global": NavElem{
			Name:   "🌍 Global",
			Short:  "🌍",
			Link:   "/global",
			Handle: Global,
		},
		"Arbit": NavElem{
			Name:   "📈 Arbitrage",
			Short:  "📈",
			Link:   "/arbit",
			Handle: Arbit,
		},
	}
}

var Config struct {
	Port                int               `json:"port"`
	DBAddress           string            `json:"db_address"`
	DiscordHook         string            `json:"discord_hook"`
	Affiliate           map[string]string `json:"affiliate"`
	AffiliatesList      []string          `json:"affiliates_list"`
	Api                 map[string]string `json:"api"`
	DiscordToken        string            `json:"discord_token"`
	DiscordAllowList    []string          `json:"discord_allowlist"`
	ArbitDefaultSellers []string          `json:"arbit_default_sellers"`
	ArbitBlockVendors   []string          `json:"arbit_block_vendors"`
	SearchBlockList     []string          `json:"search_block_list"`
	GlobalAllowList     []string          `json:"global_allow_list"`
	Patreon             struct {
		Secret map[string]string   `json:"secret"`
		Ids    map[string][]string `json:"ids"`
	} `json:"patreon"`
}

var DevMode bool
var SigCheck bool
var LastUpdate time.Time
var DatabaseLoaded bool
var Sellers []mtgban.Seller
var Vendors []mtgban.Vendor
var Infos map[string]mtgban.InventoryRecord

var Newspaper3dayDB *sql.DB
var Newspaper1dayDB *sql.DB
var ExploreDB *sql.DB

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
	if err != nil {
		return nil, err
	}
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
	showPatreonLogin := false
	if sig != "" {
		if expires < time.Now().Unix() {
			msg = ErrMsgExpired
		}
	} else {
		showPatreonLogin = true
	}

	// These values need to be set for every rendered page
	// In particular the Patreon variables are needed because the signature
	// could expire in any page, and the button url needs these parameters
	pageVars := PageVars{
		Title:        "BAN " + activeTab,
		ErrorMessage: msg,
		LastUpdate:   LastUpdate.Format(time.RFC3339),

		PatreonId:    PatreonClientId,
		PatreonURL:   PatreonHost,
		PatreonLogin: showPatreonLogin,
	}

	// Allocate a new navigation bar
	pageVars.Nav = make([]NavElem, len(DefaultNav))
	copy(pageVars.Nav, DefaultNav)

	// Enable buttons according to the enabled features
	if expires > time.Now().Unix() || (DevMode && !SigCheck) {
		for _, feat := range OrderNav {
			param, _ := GetParamFromSig(sig, feat)
			allowed, _ := strconv.ParseBool(param)
			if allowed || DevMode {
				pageVars.Nav = append(pageVars.Nav, ExtraNavs[feat])
			}
		}
	}

	mainNavIndex := 0
	for i := range pageVars.Nav {
		// Ingore the starting emoji
		if strings.HasSuffix(pageVars.Nav[i].Name, activeTab) {
			mainNavIndex = i
		}
	}
	pageVars.Nav[mainNavIndex].Active = true
	pageVars.Nav[mainNavIndex].Class = "active"
	return pageVars
}

func loadVars(cfg string) error {
	// Load from command line
	file, err := os.Open(cfg)
	if err != nil {
		return err
	}
	defer file.Close()

	d := json.NewDecoder(file)
	err = d.Decode(&Config)
	if err != nil {
		return err
	}

	// Load from env
	keyVars := []string{
		"BAN_SECRET",
	}
	for _, key := range keyVars {
		v := os.Getenv(key)
		if v == "" {
			return fmt.Errorf("%s variable not set", key)
		}
	}

	return nil
}

func main() {
	config := flag.String("cfg", "config.json", "Load configuration file")
	devMode := flag.Bool("dev", false, "Enable developer mode")
	sigCheck := flag.Bool("sig", false, "Enable signature verification")
	flag.Parse()
	DevMode = *devMode
	SigCheck = true
	if DevMode {
		SigCheck = *sigCheck
	}

	// load necessary environmental variables
	err := loadVars(*config)
	if err != nil {
		log.Fatalln(err)
	}

	Newspaper3dayDB, err = sql.Open("mysql", Config.DBAddress+"/three_day_newspaper")
	if err != nil {
		if DevMode {
			log.Println("No connection available to /three_day_newspaper DB due to", err)
		} else {
			log.Fatalln(err)
		}
	}
	Newspaper1dayDB, err = sql.Open("mysql", Config.DBAddress+"/newspaper")
	if err != nil {
		if DevMode {
			log.Println("No connection available to /newspaper DB due to", err)
		} else {
			log.Fatalln(err)
		}
	}
	ExploreDB, err = sql.Open("mysql", Config.DBAddress+"/sites")
	if err != nil {
		if DevMode {
			log.Println("No connection available to /sites DB due to", err)
		} else {
			log.Fatalln(err)
		}
	}

	// load website up
	go func() {
		var err error

		log.Println("Loading MTGJSONv5")
		err = loadDatastore()
		if err != nil {
			log.Fatalln(err)
		}

		loadScrapers(true, true)
		DatabaseLoaded = true

		// Nothing else to do if hacking around
		if DevMode {
			return
		}

		// If today's cache is missing, schedule a refresh right away
		files, err := ioutil.ReadDir(fmt.Sprintf("cache_inv/%03d", time.Now().YearDay()))
		if err != nil || len(files) < len(Sellers) {
			log.Println("Loaded inventory data too old, refreshing in the background")
			loadScrapers(true, false)
		}
		files, err = ioutil.ReadDir(fmt.Sprintf("cache_bl/%03d", time.Now().YearDay()))
		if err != nil || len(files) < len(Vendors) {
			log.Println("Loaded buylist data too old, refreshing in the background")
			loadScrapers(false, true)
		}

		// Set up new refreshes as needed
		c := cron.New()
		// refresh every day at 13:10
		c.AddFunc("10 13 * * *", func() {
			loadScrapers(true, true)
		})
		// refresh CK at every 6th hour, 10 minutes past the hour
		c.AddFunc("10 */6 * * *", loadCK)
		// refresh TCG at every 6th hour, 15 minutes past the hour
		c.AddFunc("15 */6 * * *", loadTCG)
		// refresh MKM every day at 0:00
		c.AddFunc("0 0 * * *", loadMKM)
		// refresh CSI every day at 2:10
		c.AddFunc("10 2 * * *", loadCSI)
		// refresh at 12 every day
		c.AddFunc("0 12 * * *", func() {
			log.Println("Reloading MTGJSONv5")
			err := loadDatastore()
			if err != nil {
				log.Println(err)
			}
		})
		c.Start()
	}()

	err = setupDiscord()
	if err != nil {
		if DevMode {
			log.Println("No connection to Discord due to", err)
		} else {
			log.Println("Error connecting to discord", err)
		}
	}

	// Set seed in case we need to do random operations
	rand.Seed(time.Now().UnixNano())

	// serve everything in known folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))
	http.Handle("/js/", http.StripPrefix("/js/", http.FileServer(&FileSystem{http.Dir("js")})))

	// custom redirector
	http.HandleFunc("/go/", Redirect)

	// when navigating to /home it should serve the home page
	http.Handle("/", noSigning(http.HandlerFunc(Home)))

	for _, nav := range ExtraNavs {
		http.Handle(nav.Link, enforceSigning(http.HandlerFunc(nav.Handle)))
	}

	http.Handle("/api/mtgjson/ck.json", enforceAPISigning(http.HandlerFunc(API)))
	http.HandleFunc("/favicon.ico", Favicon)
	http.HandleFunc("/auth", Auth)
	log.Fatal(http.ListenAndServe(":"+fmt.Sprint(Config.Port), nil))
}

func render(w http.ResponseWriter, tmpl string, pageVars PageVars) {
	funcMap := template.FuncMap{
		"inc": func(i, j int) int {
			return i + j
		},
		"perc": func(s string) string {
			n, _ := strconv.ParseFloat(s, 64)
			return fmt.Sprintf("%0.2f", n*100)
		},
	}

	// Give each template a name
	name := path.Base(tmpl)
	// Prefix the name passed in with templates/
	tmpl = fmt.Sprintf("templates/%s", tmpl)

	// Parse the template file held in the templates folder, add any Funcs to parsing
	t, err := template.New(name).Funcs(funcMap).ParseFiles(tmpl)
	if err != nil {
		log.Print("template parsing error: ", err)
		return
	}

	// Execute the template and pass in the variables to fill the gaps
	err = t.Execute(w, pageVars)
	if err != nil {
		log.Print("template executing error: ", err)
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/leemcloughlin/logfile"
	"golang.org/x/oauth2/google"
	"gopkg.in/Iwark/spreadsheet.v2"
	cron "gopkg.in/robfig/cron.v2"

	"github.com/kodabb/go-mtgban/mtgban"
)

type GenericCard struct {
	Name      string
	Edition   string
	SetCode   string
	Number    string
	Variant   string
	Keyrune   string
	ImageURL  string
	Foil      bool
	Etched    bool
	Reserved  bool
	Title     string
	SearchURL string
	Stocks    bool
	StocksURL string
	Printings string
}

type PageVars struct {
	Nav      []NavElem
	ExtraNav []NavElem

	PatreonId    string
	PatreonURL   string
	PatreonLogin bool
	ShowPromo    bool

	Title          string
	ErrorMessage   string
	WarningMessage string
	InfoMessage    string
	LastUpdate     string

	AllKeys      []string
	SearchQuery  string
	SearchBest   bool
	CondKeys     []string
	FoundSellers map[string]map[string][]SearchEntry
	FoundVendors map[string][]SearchEntry
	Metadata     map[string]GenericCard

	CanShowAll       bool
	CleanSearchQuery string

	ScraperShort    string
	HasAffiliate    bool
	QtyNotAvailable bool

	Arb            []Arbitrage
	ArbitOptKeys   []string
	ArbitOptNames  map[string]string
	ArbitFilters   map[string]bool
	ArbitOptNoGlob map[string]bool
	ArbitOptTests  map[string]bool
	SortOption     string
	GlobalMode     bool
	ReverseMode    bool

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
	CardHashes   []string
	CanBridge    bool
	EditionsMap  map[string]EditionEntry

	Sleepers       map[string][]string
	SleepersKeys   []string
	SleepersColors []string

	HasStocks bool

	Headers      []string
	OtherTable   [][]string
	CurrentTime  time.Time
	Uptime       string
	DiskStatus   string
	MemoryStatus string
	LatestHash   string
	CacheSize    int

	AxisLabels  []string
	Datasets    []*Dataset
	ChartID     string
	Alternative string
	StocksURL   string
	AltEtchedId string

	EditionSort []string
	EditionList map[string][]EditionEntry
	IsSealed    bool

	CompactEntries  map[string]map[string]*BanPrice
	IndexEntries    map[string]map[string]*BanPrice
	ScraperKeys     []string
	IndexKeys       []string
	SellerKeys      []string
	VendorKeys      []string
	UploadEntries   []UploadEntry
	IsBuylist       bool
	TotalEntries    map[string]float64
	EnabledSellers  []string
	EnabledVendors  []string
	CanBuylist      bool
	CanChangeStores bool
	RemoteLinkURL   string
	TotalQuantity   int
	Optimized       map[string][]string
	OptimizedTotals map[string]float64
	HighestTotal    float64
	MissingCounts   map[string]int
	MissingPrices   map[string]float64
}

type NavElem struct {
	// Whether or not this the current active tab
	Active bool

	// For subtabs, define which is the current active sub-tab
	Class string

	// Endpoint of this page
	Link string

	// Name of this page
	Name string

	// Icon or seller shorthand
	Short string

	// Response handler
	Handle func(w http.ResponseWriter, r *http.Request)

	// Which page to render
	Page string
}

var startTime = time.Now()

var DefaultNav = []NavElem{
	NavElem{
		Name:  "Home",
		Short: "🏡",
		Link:  "/",
		Page:  "home.html",
	},
}

// List of keys that may be present or not, and when present they are
// guaranteed not to be user-editable)
var OptionalFields = []string{
	"UserName",
	"UserEmail",
	"UserTier",
	"SearchDisabled",
	"SearchBuylistDisabled",
	"SearchChart",
	"SearchSealed",
	"ArbitEnabled",
	"ArbitDisabledVendors",
	"ExpEnabled",
	"NewsEnabled",
	"NewsBridgeEnabled",
	"UploadBuylistEnabled",
	"UploadChangeStoresEnabled",
	"UploadOptimizer",
	"AnyEnabled",
	"AnyExperimentsEnabled",
	"API",
	"APImode",
}

// The key matches the query parameter of the permissions defined in sign()
// These enable/disable the relevant pages
var OrderNav = []string{
	"Search",
	"Newspaper",
	"Explore",
	"Sleepers",
	"Upload",
	"Global",
	"Arbit",
	"Reverse",
	"Admin",
}

// The Loggers where each page may log to
var LogPages map[string]*log.Logger

// All the page properties
var ExtraNavs map[string]NavElem

func init() {
	ExtraNavs = map[string]NavElem{
		"Search": NavElem{
			Name:   "Search",
			Short:  "🔍",
			Link:   "/search",
			Handle: Search,
			Page:   "search.html",
		},
		"Newspaper": NavElem{
			Name:   "Newspaper",
			Short:  "🗞️",
			Link:   "/newspaper",
			Handle: Newspaper,
			Page:   "news.html",
		},
		"Explore": NavElem{
			Name:   "Explore",
			Short:  "🚠",
			Link:   "/explore",
			Handle: Explore,
			Page:   "explore.html",
		},
		"Sleepers": NavElem{
			Name:   "Sleepers",
			Short:  "💤",
			Link:   "/sleepers",
			Handle: Sleepers,
			Page:   "sleep.html",
		},
		"Upload": NavElem{
			Name:   "Upload",
			Short:  "🚢",
			Link:   "/upload",
			Handle: Upload,
			Page:   "upload.html",
		},
		"Global": NavElem{
			Name:   "Global",
			Short:  "🌍",
			Link:   "/global",
			Handle: Global,
			Page:   "arbit.html",
		},
		"Arbit": NavElem{
			Name:   "Arbitrage",
			Short:  "📈",
			Link:   "/arbit",
			Handle: Arbit,
			Page:   "arbit.html",
		},
		"Reverse": NavElem{
			Name:   "Reverse",
			Short:  "📉",
			Link:   "/reverse",
			Handle: Reverse,
			Page:   "arbit.html",
		},
		"Admin": NavElem{
			Name:   "Admin",
			Short:  "❌",
			Link:   "/admin",
			Handle: Admin,
			Page:   "admin.html",
		},
	}
}

var Config struct {
	Port                   int               `json:"port"`
	DBAddress              string            `json:"db_address"`
	DiscordHook            string            `json:"discord_hook"`
	DiscordInviteLink      string            `json:"discord_invite_link"`
	Affiliate              map[string]string `json:"affiliate"`
	AffiliatesList         []string          `json:"affiliates_list"`
	Api                    map[string]string `json:"api"`
	DiscordToken           string            `json:"discord_token"`
	DiscordAllowList       []string          `json:"discord_allowlist"`
	DevSellers             []string          `json:"dev_sellers"`
	ArbitDefaultSellers    []string          `json:"arbit_default_sellers"`
	ArbitBlockVendors      []string          `json:"arbit_block_vendors"`
	SearchRetailBlockList  []string          `json:"search_block_list"`
	SearchBuylistBlockList []string          `json:"search_buylist_block_list"`
	SleepersBlockList      []string          `json:"sleepers_block_list"`
	GlobalAllowList        []string          `json:"global_allow_list"`
	GlobalProbeList        []string          `json:"global_probe_list"`
	Patreon                struct {
		Secret map[string]string `json:"secret"`
		Emails map[string]string `json:"emails"`
	} `json:"patreon"`
	ApiUserSecrets    map[string]string `json:"api_user_secrets"`
	GoogleCredentials string            `json:"google_credentials"`
}

var DevMode bool
var SigCheck bool
var LogDir string
var LastUpdate time.Time
var DatabaseLoaded bool
var Sellers []mtgban.Seller
var Vendors []mtgban.Vendor
var Infos map[string]mtgban.InventoryRecord

var SealedEditionsSorted []string
var SealedEditionsList map[string][]EditionEntry
var AllEditionsKeys []string
var AllEditionsMap map[string]EditionEntry

var Newspaper3dayDB *sql.DB
var Newspaper1dayDB *sql.DB
var ExploreDB *sql.DB

var GoogleDocsClient *http.Client

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
	exp := GetParamFromSig(sig, "Expires")
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
			param := GetParamFromSig(sig, feat)
			allowed, _ := strconv.ParseBool(param)
			if allowed || DevMode {
				pageVars.Nav = append(pageVars.Nav, ExtraNavs[feat])
			}
		}
	}

	mainNavIndex := 0
	for i := range pageVars.Nav {
		if pageVars.Nav[i].Name == activeTab {
			mainNavIndex = i
			break
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

func openDBs() (err error) {
	Newspaper3dayDB, err = sql.Open("mysql", Config.DBAddress+"/three_day_newspaper")
	if err != nil {
		return err
	}
	Newspaper1dayDB, err = sql.Open("mysql", Config.DBAddress+"/newspaper")
	if err != nil {
		return err
	}
	ExploreDB, err = sql.Open("mysql", Config.DBAddress+"/sites")
	if err != nil {
		return err
	}

	return nil
}

func loadGoogleCredentials(credentials string) (*http.Client, error) {
	data, err := ioutil.ReadFile(credentials)
	if err != nil {
		return nil, err
	}

	conf, err := google.JWTConfigFromJSON(data, spreadsheet.Scope)
	if err != nil {
		return nil, err
	}

	return conf.Client(context.Background()), nil
}

const DefaultConfigPath = "config.json"

func main() {
	config := flag.String("cfg", DefaultConfigPath, "Load configuration file")
	devMode := flag.Bool("dev", false, "Enable developer mode")
	sigCheck := flag.Bool("sig", false, "Enable signature verification")
	logdir := flag.String("log", "logs", "Directory for scrapers logs")
	flag.Parse()
	DevMode = *devMode
	SigCheck = true
	if DevMode {
		SigCheck = *sigCheck
	}
	LogDir = *logdir

	// load necessary environmental variables
	err := loadVars(*config)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = os.Stat(LogDir)
	if errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(LogDir, 0700)
	}
	if err != nil {
		log.Fatalln(err)
	}
	LogPages = map[string]*log.Logger{}

	GoogleDocsClient, err = loadGoogleCredentials(Config.GoogleCredentials)
	if err != nil {
		if DevMode {
			log.Println("Error creating a Google client:", err)
		} else {
			log.Fatalln(err)
		}
	}

	err = openDBs()
	if err != nil {
		if DevMode {
			log.Println("Error opening databases:", err)
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

		loadScrapers()
		DatabaseLoaded = true

		// Nothing else to do if hacking around
		if DevMode {
			return
		}

		// Set up new refreshes as needed
		c := cron.New()
		// refresh every day at 13:10
		c.AddFunc("10 13 * * *", loadScrapers)
		// refresh CK at every 6th hour, 10 minutes past the hour
		c.AddFunc("10 */6 * * *", reloadCK)
		// refresh TCG at every 6th hour, 15 minutes past the hour
		c.AddFunc("15 */6 * * *", reloadTCG)
		// refresh CSI every day at 2:10
		c.AddFunc("10 2 * * *", reloadCSI)
		// refresh SCG every day at 1:11
		c.AddFunc("11 1 * * *", reloadSCG)
		// refresh at 12 every day
		c.AddFunc("0 12 * * *", func() {
			log.Println("Reloading MTGJSONv5")
			err := loadDatastore()
			if err != nil {
				log.Println(err)
			}
		})

		// Every seven days in a month, clean up the csv cache
		c.AddFunc("0 0 */7 * *", deleteOldCache)

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
	http.HandleFunc("/discord", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, Config.DiscordInviteLink, http.StatusFound)
	})

	// when navigating to /home it should serve the home page
	http.Handle("/", noSigning(http.HandlerFunc(Home)))

	for key, nav := range ExtraNavs {
		// Set up logging
		logFile, err := logfile.New(&logfile.LogFile{
			FileName:    path.Join(LogDir, key+".log"),
			MaxSize:     500 * 1024,
			Flags:       logfile.FileOnly,
			OldVersions: 2,
		})
		if err != nil {
			log.Printf("Failed to create logFile for %s: %s", key, err)
			LogPages[key] = log.New(os.Stderr, "", log.LstdFlags)
		} else {
			LogPages[key] = log.New(logFile, "", log.LstdFlags)
		}

		// Set up the handler
		http.Handle(nav.Link, enforceSigning(http.HandlerFunc(nav.Handle)))
	}

	http.Handle("/api/mtgban/", enforceAPISigning(http.HandlerFunc(PriceAPI)))
	http.Handle("/api/mtgjson/ck.json", enforceAPISigning(http.HandlerFunc(API)))
	http.Handle("/api/cardkingdom/pricelist.json", noSigning(http.HandlerFunc(CKMirrorAPI)))
	http.HandleFunc("/favicon.ico", Favicon)
	http.HandleFunc("/auth", Auth)
	log.Fatal(http.ListenAndServe(":"+fmt.Sprint(Config.Port), nil))
}

func render(w http.ResponseWriter, tmpl string, pageVars PageVars) {
	funcMap := template.FuncMap{
		"inc": func(i, j int) int {
			return i + j
		},
		"dec": func(i, j int) int {
			return i - j
		},
		"print_perc": func(s string) string {
			n, _ := strconv.ParseFloat(s, 64)
			return fmt.Sprintf("%0.2f %%", n*100)
		},
		"print_price": func(s string) string {
			n, _ := strconv.ParseFloat(s, 64)
			return fmt.Sprintf("$ %0.2f", n)
		},
		"scraper_name": func(s string) string {
			return ScraperNames[s]
		},
		"banprice2price": func(p *BanPrice) float64 {
			if p == nil {
				return 0
			}
			if p.Regular != 0 {
				return p.Regular
			}
			if p.Foil != 0 {
				return p.Foil
			}
			return p.Etched
		},
		"slice_has": func(s []string, p string) bool {
			return SliceStringHas(s, p)
		},
		"triple_column_start": func(i int, length int) bool {
			return i == 0 || i == length/3 || i == length*2/3
		},
		"triple_column_end": func(i int, length int) bool {
			return i == length/3-1 || i == length*2/3-1 || i == length-1
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

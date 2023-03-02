package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"database/sql"

	"cloud.google.com/go/storage"
	_ "github.com/go-sql-driver/mysql"
	"github.com/leemcloughlin/logfile"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"gopkg.in/Iwark/spreadsheet.v2"
	cron "gopkg.in/robfig/cron.v2"

	"github.com/mtgban/go-mtgban/mtgban"
)

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
	SearchSort   string
	CondKeys     []string
	FoundSellers map[string]map[string][]SearchEntry
	FoundVendors map[string]map[string][]SearchEntry
	Metadata     map[string]GenericCard
	PromoTags    []string
	NoSort       bool

	CanShowAll       bool
	CleanSearchQuery string

	ScraperShort    string
	HasAffiliate    bool
	QtyNotAvailable bool
	CanDownloadCSV  bool

	Arb            []Arbitrage
	ArbitOptKeys   []string
	ArbitOptConfig map[string]FilterOpt
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
	HasStocks    bool
	HasSypList   bool
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
	EditionsMap  map[string]EditionEntry

	CanFilterByPrice bool
	FilterMinPrice   float64
	FilterMaxPrice   float64

	CanFilterByPercentage bool
	FilterMinPercChange   float64
	FilterMaxPercChange   float64

	Sleepers       map[string][]string
	SleepersKeys   []string
	SleepersColors []string

	Headers      []string
	OtherTable   [][]string
	CurrentTime  time.Time
	Uptime       string
	DiskStatus   string
	MemoryStatus string
	LatestHash   string
	CacheSize    int
	Tiers        []string
	DemoKey      string

	AxisLabels  []string
	Datasets    []*Dataset
	ChartID     string
	Alternative string
	StocksURL   string
	AltEtchedId string

	EditionSort []string
	EditionList map[string][]EditionEntry
	IsSealed    bool
	IsSets      bool
	TotalSets   int
	TotalCards  int
	TotalUnique int

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
	CanOptimize     bool
	CanChangeStores bool
	RemoteLinkURL   string
	TotalQuantity   int
	Optimized       map[string][]OptimizedUploadEntry
	OptimizedTotals map[string]float64
	HighestTotal    float64
	MissingCounts   map[string]int
	MissingPrices   map[string]float64
	ResultPrices    map[string]map[string]float64

	OptimizedEditions map[string][]OptimizedUploadEntry
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

	// Whether this tab should always be enabled in DevMode
	AlwaysOnForDev bool
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
	"SearchSealed",
	"SearchDownloadCSV",
	"ArbitEnabled",
	"ArbitDisabledVendors",
	"NewsEnabled",
	"UploadBuylistEnabled",
	"UploadChangeStoresEnabled",
	"UploadOptimizer",
	"AnyEnabled",
	"AnyExperimentsEnabled",
	"APImode",
}

// The key matches the query parameter of the permissions defined in sign()
// These enable/disable the relevant pages
var OrderNav = []string{
	"Search",
	"Newspaper",
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

			AlwaysOnForDev: true,
		},
	}
}

var Config struct {
	Port                   string            `json:"port"`
	DBAddress              string            `json:"db_address"`
	DiscordHook            string            `json:"discord_hook"`
	DiscordNotifHook       string            `json:"discord_notif_hook"`
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

	ACL map[string]map[string]map[string]string `json:"acl"`

	Uploader struct {
		ServiceAccount string `json:"service_account"`
		BucketName     string `json:"bucket_name"`
		ProjectID      string `json:"project_id"`
		DatasetID      string `json:"dataset_id"`
	} `json:"uploader"`

	Scrapers map[string][]struct {
		HasRedis   bool   `json:"has_redis,omitempty"`
		RedisIndex int    `json:"redis_index,omitempty"`
		TableName  string `json:"table_name"`
		mtgban.ScraperInfo
	} `json:"scrapers"`

	/* The location of the configuation file */
	filePath string
}

var DevMode bool
var SigCheck bool
var SkipInitialRefresh bool
var BenchMode bool
var LogDir string
var LastUpdate string
var DatabaseLoaded bool
var Sellers []mtgban.Seller
var Vendors []mtgban.Vendor
var Infos map[string]mtgban.InventoryRecord

var SealedEditionsSorted []string
var SealedEditionsList map[string][]EditionEntry
var AllEditionsKeys []string
var AllEditionsMap map[string]EditionEntry
var TreeEditionsKeys []string
var TreeEditionsMap map[string][]EditionEntry
var ReprintsKeys []string
var ReprintsMap map[string][]ReprintEntry

var TotalSets, TotalCards, TotalUnique int

var Newspaper3dayDB *sql.DB
var Newspaper1dayDB *sql.DB

var GoogleDocsClient *http.Client
var GCSBucketClient *storage.Client

const (
	DefaultConfigPort = "8080"
	DefaultSecret     = "NotVerySecret!"
)

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
		LastUpdate:   LastUpdate,

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
			if DevMode && ExtraNavs[feat].AlwaysOnForDev {
				allowed = true
			}
			if allowed || (DevMode && !SigCheck) {
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

	Config.filePath = cfg

	if Config.Port == "" {
		Config.Port = DefaultConfigPort
	}

	// Load from env
	v := os.Getenv("BAN_SECRET")
	if v == "" {
		log.Printf("BAN_SECRET not set, using a default one")
		os.Setenv("BAN_SECRET", DefaultSecret)
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
	return nil
}

func loadGoogleCredentials(credentials string) (*http.Client, error) {
	data, err := os.ReadFile(credentials)
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
	skipInitialRefresh := flag.Bool("skip", false, "Skip initial refresh")
	logdir := flag.String("log", "logs", "Directory for scrapers logs")
	port := flag.String("port", "", "Override server port")

	flag.Parse()
	DevMode = *devMode
	SkipInitialRefresh = *skipInitialRefresh
	SigCheck = true
	if DevMode {
		SigCheck = *sigCheck
	}
	LogDir = *logdir

	// load necessary environmental variables
	err := loadVars(*config)
	if err != nil {
		log.Fatalln("unable to load config file:", err)
	}
	if *port != "" {
		Config.Port = *port
	}

	_, err = os.Stat(LogDir)
	if errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(LogDir, 0700)
	}
	if err != nil {
		log.Fatalln("unable to create necessary folders", err)
	}
	LogPages = map[string]*log.Logger{}

	GoogleDocsClient, err = loadGoogleCredentials(Config.GoogleCredentials)
	if err != nil {
		if DevMode {
			log.Println("error creating a Google client:", err)
		} else {
			log.Fatalln("error creating a Google client:", err)
		}
	}

	GCSBucketClient, err = storage.NewClient(context.Background(), option.WithCredentialsFile(Config.Uploader.ServiceAccount))
	if err != nil {
		log.Fatalln("error creating the GCSBucketClient:", err)
	}

	err = openDBs()
	if err != nil {
		if DevMode {
			log.Println("error opening databases:", err)
		} else {
			log.Fatalln("error opening databases:", err)
		}
	}

	// load website up
	go func() {
		go func() {
			log.Println("Loading MTGJSONv5")
			err := loadDatastore()
			if err != nil {
				log.Fatalln("error loading mtgjson:", err)
			}
		}()

		log.Println("Loading cache")
		err := startup()
		if err != nil {
			log.Fatalln("error loading cache:", err)
		}

		log.Println("Loading BQ")
		err = loadBQ()
		if err != nil {
			log.Fatalln("error loading bq:", err)
		}

		// Nothing else to do if hacking around
		if DevMode {
			return
		}

		// Set up new refreshes as needed
		c := cron.New()

		// Times are in UTC
		// Reload data from BQ every 3 hours
		c.AddFunc("0 */3 * * *", loadBQcron)

		// Refresh everything daily at 2am (after MTGJSON update)
		c.AddFunc("35 11 * * *", loadScrapers)
		// Refresh CK at every 4th hour, 40 minutes past the hour (six times in total)
		c.AddFunc("40 */4 * * *", reloadCK)
		// Refresh TCG at every 4th hour, 45 minutes past the hour (six times in total)
		c.AddFunc("45 */4 * * *", reloadTCG)
		// Refresh SCG every day at 2:15pm (twice in total)
		c.AddFunc("15 14 * * *", reloadSCG)

		// MTGJSON builds go live 7am EST, pull the update 30 minutes after
		c.AddFunc("30 11 * * *", func() {
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
		log.Println("Error connecting to discord", err)
	}

	// Set seed in case we need to do random operations
	rand.Seed(time.Now().UnixNano())

	// serve everything in known folders as a file
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(&FileSystem{http.Dir("css")})))
	http.Handle("/img/", http.StripPrefix("/img/", http.FileServer(&FileSystem{http.Dir("img")})))
	http.Handle("/js/", http.StripPrefix("/js/", http.FileServer(&FileSystem{http.Dir("js")})))

	// custom redirector
	http.HandleFunc("/go/", Redirect)
	http.HandleFunc("/random", RandomSearch)
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

	http.Handle("/sets", enforceSigning(http.HandlerFunc(Search)))
	http.Handle("/sealed", enforceSigning(http.HandlerFunc(Search)))

	http.Handle("/api/mtgban/", enforceAPISigning(http.HandlerFunc(PriceAPI)))
	http.Handle("/api/mtgjson/ck.json", enforceAPISigning(http.HandlerFunc(API)))
	http.Handle("/api/tcgplayer/lastsold/", enforceSigning(http.HandlerFunc(TCGLastSoldAPI)))
	http.Handle("/api/cardkingdom/pricelist.json", noSigning(http.HandlerFunc(CKMirrorAPI)))
	http.HandleFunc("/favicon.ico", Favicon)
	http.HandleFunc("/auth", Auth)
	log.Fatal(http.ListenAndServe(":"+Config.Port, nil))
}

func render(w http.ResponseWriter, tmpl string, pageVars PageVars) {
	funcMap := template.FuncMap{
		"inc": func(i, j int) int {
			return i + j
		},
		"dec": func(i, j int) int {
			return i - j
		},
		"mul": func(i float64, j int) float64 {
			return i * float64(j)
		},
		"print_perc": func(s string) string {
			n, _ := strconv.ParseFloat(s, 64)
			return fmt.Sprintf("%0.2f %%", n*100)
		},
		"print_price": func(s string) string {
			n, _ := strconv.ParseFloat(s, 64)
			return fmt.Sprintf("$ %0.2f", n)
		},
		"seller_name": func(s string) string {
			for _, scraperData := range Config.Scrapers["sellers"] {
				if s == scraperData.Shorthand {
					return scraperData.Name
				}
			}
			return ""
		},
		"vendor_name": func(s string) string {
			for _, scraperData := range Config.Scrapers["vendors"] {
				if s == scraperData.Shorthand {
					return scraperData.Name
				}
			}
			return ""
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
		"tolower": func(s string) string {
			return strings.ToLower(s)
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

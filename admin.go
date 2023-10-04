package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"

	git "github.com/go-git/go-git/v5"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/mackerelio/go-osstat/memory"
	"golang.org/x/sys/unix"
)

const (
	mtgjsonURL = "https://mtgjson.com/api/v5/AllPrintings.json"
	GoFullPath = "/usr/local/go/bin/go"
)

var BuildCommit = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}()

func Admin(w http.ResponseWriter, r *http.Request) {
	sig := getSignatureFromCookies(r)

	pageVars := genPageNav("Admin", sig)

	msg := r.FormValue("msg")
	if msg != "" {
		pageVars.InfoMessage = msg
	}

	refresh := r.FormValue("refresh")
	if refresh != "" {
		key, found := ScraperMap[refresh]
		if !found {
			pageVars.InfoMessage = refresh + " not found"
		}
		if key != "" {
			_, found := ScraperOptions[key]
			if !found {
				pageVars.InfoMessage = key + " not found"
			} else {
				// Strip the request parameter to avoid accidental repeats
				// and to give a chance to table to update
				r.URL.RawQuery = ""
				if ScraperOptions[key].Busy {
					v := url.Values{
						"msg": {key + " is already being refreshed"},
					}
					r.URL.RawQuery = v.Encode()
				} else if len(ScraperOptions[key].Keepers) > 0 {
					go reloadMarket(key)
				} else {
					go reloadSingle(key)
				}

				http.Redirect(w, r, r.URL.String(), http.StatusFound)
				return
			}
		}
	}
	cloud := r.FormValue("cloud")
	cloud_bl := r.FormValue("cloud_bl")
	if cloud != "" || cloud_bl != "" {
		if cloud != "" {
			configMutex.RLock()
			config, found := SellersConfigMap[cloud]
			configMutex.RUnlock()
			if !found {
				pageVars.InfoMessage = cloud + " not found"
			} else {
				seller, err := downloadSeller(config.Path)
				if err != nil {
					v := url.Values{
						"msg": {cloud + " " + err.Error()},
					}
					r.URL.RawQuery = v.Encode()
				} else {
					for i := range Sellers {
						if Sellers[i] != nil && Sellers[i].Info().Shorthand == seller.Info().Shorthand {
							Sellers[i] = seller
						}
					}
				}
			}
		} else {
			configMutex.RLock()
			config, found := VendorsConfigMap[cloud_bl]
			configMutex.RUnlock()
			if !found {
				pageVars.InfoMessage = cloud_bl + " not found"
			} else {
				vendor, err := downloadVendor(config.Path)
				if err != nil {
					v := url.Values{
						"msg": {cloud_bl + " " + err.Error()},
					}
					r.URL.RawQuery = v.Encode()
				} else {
					for i := range Vendors {
						if Vendors[i] != nil && Vendors[i].Info().Shorthand == vendor.Info().Shorthand {
							Vendors[i] = vendor
						}
					}
				}
			}
		}
	}

	logs := r.FormValue("logs")
	if logs != "" {
		key, found := ScraperMap[logs]
		if !found {
			pageVars.InfoMessage = key + " not found"
		}
		log.Println(path.Join(LogDir, key+".log"))
		http.ServeFile(w, r, path.Join(LogDir, key+".log"))
		return
	}

	spoof := r.FormValue("spoof")
	if spoof != "" {
		baseURL := getBaseURL(r)
		sig := sign(baseURL, spoof, nil)

		// Overwrite the current signature
		putSignatureInCookies(w, r, sig)

		http.Redirect(w, r, baseURL, http.StatusFound)
		return
	}

	reboot := r.FormValue("reboot")
	doReboot := false
	var v url.Values
	switch reboot {
	case "infos":
		v = url.Values{}
		v.Set("msg", "Refreshing Infos in the background...")
		doReboot = true
		go loadInfos()

	case "mtgjson":
		v = url.Values{}
		v.Set("msg", "Reloading MTGJSON in the background...")
		doReboot = true

		go func() {
			// Load Allprintings.json remotely
			log.Println("Retrieving the latest version of mtgjson")
			resp, err := cleanhttp.DefaultClient().Get(mtgjsonURL)
			if err != nil {
				log.Println(err)
				return
			}
			defer resp.Body.Close()

			// Create a new file, copy contents over then move new file over the old one
			log.Println("Installing the new mtgjson version")
			fo, err := os.Create(AllPrintingsFileName + "new")
			if err != nil {
				log.Println(err)
				return
			}
			defer fo.Close()

			_, err = io.Copy(fo, resp.Body)
			if err != nil {
				log.Println(err)
				return
			}
			fo.Close()

			err = os.Rename(AllPrintingsFileName+"new", AllPrintingsFileName)
			if err != nil {
				log.Println(err)
				return
			}

			// Reload the newly created file
			log.Println("Loading the new mtgjson version")
			err = loadDatastore()
			if err != nil {
				log.Println(err)
				return
			}
			log.Println("New mtgjson is ready")
		}()

	case "update":
		v = url.Values{}
		v.Set("msg", "Deploying...")
		doReboot = true

		go func() {
			log.Println("Pulling new code")
			_, err := pullCode()
			if err != nil {
				log.Println(err)
				return
			}

			log.Println("Building new code")
			out, err := build()
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(out)

			log.Println("Restarting")
			os.Exit(0)
		}()

	case "build":
		v = url.Values{}
		doReboot = true

		out, err := build()
		if err != nil {
			log.Println(err)
		}
		v.Set("msg", out)

	case "code":
		v = url.Values{}
		v.Set("msg", "Pulling from master...")
		doReboot = true

		go pullCode()

	case "cache":
		v = url.Values{}
		v.Set("msg", "Deleting old cache...")
		doReboot = true

		go deleteOldCache()

	case "config":
		v = url.Values{}
		v.Set("msg", "New config loaded!")
		doReboot = true

		err := loadVars(DefaultConfigPath)
		if err != nil {
			v.Set("msg", "Failed to reload config: "+err.Error())
		}

	case "scrapers":
		v = url.Values{}
		v.Set("msg", "Reloading scrapers in the background...")
		doReboot = true

		skip := false
		for key, opt := range ScraperOptions {
			if opt.Busy {
				v.Set("msg", "Cannot reload everything while "+key+" is refreshing")
				skip = true
				break
			}
		}

		if !skip {
			go loadScrapers()
		}

	case "cloud":
		v = url.Values{}
		v.Set("msg", "Reloading scrapers from the cloud in the background...")
		doReboot = true

		skip := false
		for key, opt := range ScraperOptions {
			if opt.Busy {
				v.Set("msg", "Cannot reload everything while "+key+" is refreshing")
				skip = true
				break
			}
		}

		if !skip {
			go loadScrapersNG()
		}

	case "server":
		v = url.Values{}
		v.Set("msg", "Restarting the server...")
		doReboot = true

		// Let the system restart the server
		go func() {
			time.Sleep(5 * time.Second)
			log.Println("Admin requested server restart")
			os.Exit(0)
		}()

	case "newKey":
		v = url.Values{}
		doReboot = true

		user := r.FormValue("user")
		dur := r.FormValue("duration")
		duration, _ := strconv.Atoi(dur)

		key, err := generateAPIKey(getBaseURL(r), user, time.Duration(duration)*24*time.Hour)
		msg := key
		if err != nil {
			msg = "error: " + err.Error()
		}

		v.Set("msg", msg)
	}
	if doReboot {
		r.URL.RawQuery = v.Encode()
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
		return
	}

	pageVars.Headers = []string{
		"", "Name", "Id+Logs", "Last Update", "Entries", "Status",
	}
	for i := range Sellers {
		if Sellers[i] == nil {
			row := []string{
				fmt.Sprintf("Error at Seller %d", i), "", "", "", "",
			}
			pageVars.Table = append(pageVars.Table, row)
			continue
		}

		scraperOptions, found := ScraperOptions[ScraperMap[Sellers[i].Info().Shorthand]]
		if !found {
			continue
		}

		lastUpdate := Sellers[i].Info().InventoryTimestamp.Format(time.Stamp)

		inv, _ := Sellers[i].Inventory()

		status := "✅"
		if scraperOptions.Busy {
			status = "🔶"
		} else if len(inv) == 0 {
			status = "🔴"
		}

		row := []string{
			Sellers[i].Info().Name,
			Sellers[i].Info().Shorthand,
			lastUpdate,
			fmt.Sprint(len(inv)),
			status,
		}

		pageVars.Table = append(pageVars.Table, row)
	}

	for i := range Vendors {
		if Vendors[i] == nil {
			row := []string{
				fmt.Sprintf("Error at Vendor %d", i), "", "", "", "",
			}
			pageVars.OtherTable = append(pageVars.Table, row)
			continue
		}

		scraperOptions, found := ScraperOptions[ScraperMap[Vendors[i].Info().Shorthand]]
		if !found {
			continue
		}

		lastUpdate := Vendors[i].Info().BuylistTimestamp.Format(time.Stamp)

		bl, _ := Vendors[i].Buylist()

		status := "✅"
		if scraperOptions.Busy {
			status = "🔶"
		} else if len(bl) == 0 {
			status = "🔴"
		}

		row := []string{
			Vendors[i].Info().Name,
			Vendors[i].Info().Shorthand,
			lastUpdate,
			fmt.Sprint(len(bl)),
			status,
		}

		pageVars.OtherTable = append(pageVars.OtherTable, row)
	}

	var tiers []string
	for tierName := range Config.ACL {
		tiers = append(tiers, tierName)
	}
	sort.Slice(tiers, func(i, j int) bool {
		return tiers[i] < tiers[j]
	})

	pageVars.Tiers = tiers
	pageVars.Uptime = uptime()
	pageVars.DiskStatus = disk()
	pageVars.MemoryStatus = mem()
	pageVars.LatestHash = BuildCommit
	pageVars.CurrentTime = time.Now()
	pageVars.DemoKey = url.QueryEscape(getDemoKey(getBaseURL(r)))

	render(w, "admin.html", pageVars)
}

func pullCode() (string, error) {
	r, err := git.PlainOpen(".")
	if err != nil {
		return "", err
	}

	// Get the working directory for the repository
	w, err := r.Worktree()
	if err != nil {
		return "", err
	}

	// Pull the latest changes from the origin remote and merge into the current branch
	err = w.Pull(&git.PullOptions{
		RemoteName: "origin",
		Auth: &githttp.BasicAuth{
			Username: "xxx", // Anything but empty string
			Password: Config.Api["github_access_token"],
		},
		Progress: os.Stdout,
	})
	if err != nil {
		return "", err
	}

	// Print the latest commit that was just pulled
	ref, err := r.Head()
	if err != nil {
		return "", err
	}

	return ref.Hash().String(), nil
}

func build() (string, error) {
	cmd := exec.Command(GoFullPath, "build")
	var out bytes.Buffer
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return "", nil
	}
	if out.Len() == 0 {
		return "Build successful", nil
	}
	return out.String(), nil
}

func deleteOldCache() {
	var size int64

	log.Println("Wiping cache")
	for _, directory := range []string{"cache_inv/", "cache_bl"} {
		// Open the directory and read all its files.
		dirRead, err := os.Open(directory)
		if err != nil {
			continue
		}
		defer dirRead.Close()

		dirFiles, err := dirRead.Readdir(0)
		if err != nil {
			continue
		}

		for _, subdir := range dirFiles {
			// Skip most recent entries
			dayTag := fmt.Sprintf("%03d", time.Now().YearDay()-1)[:2]
			if strings.HasPrefix(subdir.Name(), dayTag) {
				continue
			}

			// Read and list subdirectories
			subPath := path.Join(directory, subdir.Name())
			subDirRead, err := os.Open(subPath)
			if err != nil {
				continue
			}
			defer subDirRead.Close()

			subDirFiles, err := subDirRead.Readdir(0)
			if err != nil {
				continue
			}

			// Loop over the directory's files and remove them
			for _, files := range subDirFiles {
				size += files.Size()
				fullPath := path.Join(directory, subdir.Name(), files.Name())
				log.Println("Deleting", fullPath)
				os.Remove(fullPath)
			}

			// Remove containing directory
			log.Println("Deleting", subPath)
			os.Remove(subPath)
		}
	}
	log.Printf("Cache is wiped, %dkb freed", size/1024)
}

// Custom time.Duration format to print days as well
func uptime() string {
	since := time.Since(startTime)
	days := int(since.Hours() / 24)
	hours := int(since.Hours()) % 24
	minutes := int(since.Minutes()) % 60
	seconds := int(since.Seconds()) % 60
	return fmt.Sprintf("%d days, %02d:%02d:%02d", days, hours, minutes, seconds)
}

func disk() string {
	wd, err := os.Getwd()
	if err != nil {
		return "N/A"
	}
	var stat unix.Statfs_t
	unix.Statfs(wd, &stat)

	total := stat.Blocks * uint64(stat.Bsize)
	avail := stat.Bavail * uint64(stat.Bsize)
	used := total - avail

	return fmt.Sprintf("%.2f%% of %.2fGB", float64(used)/float64(total)*100, float64(total)/1024/1024/1024)
}

func mem() string {
	memData, err := memory.Get()
	if err != nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%% of %.2fGB", float64(memData.Used)/float64(memData.Total)*100, float64(memData.Total)/1024/1024/1024)
}

const (
	DefaultAPIDemoKeyDuration = 30 * 24 * time.Hour
	DefaultAPIDemoUser        = "demo@mtgban.com"
)

func getDemoKey(link string) string {
	key, _ := generateAPIKey(link, DefaultAPIDemoUser, DefaultAPIDemoKeyDuration)
	return key
}

var apiUsersMutex sync.RWMutex

func generateAPIKey(link, user string, duration time.Duration) (string, error) {
	if user == "" {
		return "", errors.New("missing user")
	}

	apiUsersMutex.RLock()
	key, found := Config.ApiUserSecrets[user]
	apiUsersMutex.RUnlock()

	if !found {
		key = randomString(15)
		apiUsersMutex.Lock()
		Config.ApiUserSecrets[user] = key
		apiUsersMutex.Unlock()

		file, err := os.Create(Config.filePath)
		if err != nil {
			return "", err
		}
		defer file.Close()

		e := json.NewEncoder(file)
		// Avoids & -> \u0026 and similar
		e.SetEscapeHTML(false)
		e.SetIndent("", "    ")
		err = e.Encode(&Config)
		if err != nil {
			return "", err
		}
	}

	v := url.Values{}
	v.Set("API", "ALL_ACCESS")
	v.Set("APImode", "all")
	v.Set("UserEmail", user)

	var exp string
	if duration != 0 {
		expires := time.Now().Add(duration)
		exp = fmt.Sprintf("%d", expires.Unix())
		v.Set("Expires", exp)
	}

	data := fmt.Sprintf("GET%s%s%s", exp, link, v.Encode())
	sig := signHMACSHA1Base64([]byte(key), []byte(data))

	v.Set("Signature", sig)
	return base64.StdEncoding.EncodeToString([]byte(v.Encode())), nil
}

// 32-126 are the printable characters in ashii, 33 excludes space
func randomString(l int) string {
	rand.Seed(time.Now().UnixNano())
	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(33 + rand.Intn(126-33))
	}
	return string(bytes)
}

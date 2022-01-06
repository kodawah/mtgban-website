package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"golang.org/x/oauth2"
)

var PatreonHost string

const (
	DefaultHost              = "www.mtgban.com"
	DefaultSignatureDuration = 11 * 24 * time.Hour
)

const (
	PatreonClientId = "VrjStFvhtp7HhF1xItHm83FMY7PK3nptpls1xVkYL5IDufXNVW4Xb-pHPXBIuWZ4"

	PatreonTokenURL    = "https://www.patreon.com/api/oauth2/token"
	PatreonIdentityURL = "https://www.patreon.com/api/oauth2/v2/identity?include=memberships&fields%5Buser%5D=email,first_name,full_name,image_url,last_name,social_connections,thumb_url,url,vanity"
	PatreonMemberURL   = "https://www.patreon.com/api/oauth2/v2/members/"
	PatreonMemberOpts  = "?include=currently_entitled_tiers&fields%5Btier%5D=title"
)

const (
	ErrMsg        = "Join the BAN Community and gain access to exclusive tools!"
	ErrMsgPlus    = "Increase your pledge to gain access to this feature!"
	ErrMsgDenied  = "Something went wrong while accessing this page"
	ErrMsgExpired = "You've been logged out"
	ErrMsgRestart = "Website is restarting, please try again in a few minutes"
	ErrMsgUseAPI  = "Slow down, you're making too many requests! For heavy data use consider the BAN API"
)

func getUserToken(code, baseURL, ref string) (string, error) {
	clientId := PatreonClientId
	secret := Config.Patreon.Secret["ban"]

	resp, err := cleanhttp.DefaultClient().PostForm(PatreonTokenURL, url.Values{
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"client_id":     {clientId},
		"client_secret": {secret},
		"redirect_uri":  {baseURL + "/auth"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var userTokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Expires      int    `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	err = json.Unmarshal(data, &userTokens)
	if err != nil {
		return "", err
	}

	return userTokens.AccessToken, nil
}

type PatreonUserData struct {
	UserIds  []string
	FullName string
	Email    string
}

// Retrieve a user id for each membership of the current user
func getUserIds(tc *http.Client) (*PatreonUserData, error) {
	resp, err := tc.Get(PatreonIdentityURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userData struct {
		Errors []struct {
			Title    string `json:"title"`
			CodeName string `json:"code_name"`
		} `json:"errors"`
		Data struct {
			Attributes struct {
				Email    string `json:"email"`
				FullName string `json:"full_name"`
			} `json:"attributes"`
			Relationships struct {
				Memberships struct {
					Data []struct {
						Id   string `json:"id"`
						Type string `json:"type"`
					} `json:"data"`
				} `json:"memberships"`
			} `json:"relationships"`
			IdV1 string `json:"id"`
		} `json:"data"`
	}

	LogPages["Admin"].Println(string(data))
	err = json.Unmarshal(data, &userData)
	if err != nil {
		return nil, err
	}
	if len(userData.Errors) > 0 {
		return nil, errors.New(userData.Errors[0].CodeName)
	}

	userIds := []string{userData.Data.IdV1}
	for _, memberData := range userData.Data.Relationships.Memberships.Data {
		if memberData.Type == "member" {
			userIds = append(userIds, memberData.Id)
			break
		}
	}

	return &PatreonUserData{
		UserIds:  userIds,
		FullName: userData.Data.Attributes.FullName,
		Email:    strings.ToLower(userData.Data.Attributes.Email),
	}, nil
}

func getUserTier(tc *http.Client, userId string) (string, error) {
	resp, err := tc.Get(PatreonMemberURL + userId + PatreonMemberOpts)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var membershipData struct {
		Errors []struct {
			Title    string `json:"title"`
			CodeName string `json:"code_name"`
			Detail   string `json:"detail"`
		} `json:"errors"`
		Data struct {
			Relationships struct {
				CurrentlyEntitledTiers struct {
					Data []struct {
						Id   string `json:"id"`
						Type string `json:"type"`
					} `json:"data"`
				} `json:"currently_entitled_tiers"`
			} `json:"relationships"`
		} `json:"data"`
		Included []struct {
			Attributes struct {
				Title string `json:"title"`
			} `json:"attributes"`
			Id   string `json:"id"`
			Type string `json:"type"`
		} `json:"included"`
	}
	tierId := ""
	tierTitle := ""
	LogPages["Admin"].Println(string(data))
	err = json.Unmarshal(data, &membershipData)
	if err != nil {
		return "", err
	}
	if len(membershipData.Errors) > 0 {
		return "", errors.New(membershipData.Errors[0].Detail)
	}

	for _, tierData := range membershipData.Data.Relationships.CurrentlyEntitledTiers.Data {
		if tierData.Type == "tier" {
			tierId = tierData.Id
			break
		}
	}
	for _, tierData := range membershipData.Included {
		if tierData.Type == "tier" && tierId == tierData.Id {
			tierTitle = tierData.Attributes.Title
		}
	}
	if tierTitle == "" {
		return "", errors.New("empty tier title")
	}

	return tierTitle, nil
}

// Retrieve the main url, mostly for Patron auth -- we can't use the one provided
// by the url since it can be relative and thus empty
func getBaseURL(r *http.Request) string {
	host := r.Host
	if host == "localhost:"+fmt.Sprint(Config.Port) && !DevMode {
		host = DefaultHost
	}
	baseURL := "http://" + host
	if r.TLS != nil {
		baseURL = strings.Replace(baseURL, "http", "https", 1)
	}
	return baseURL
}

func Auth(w http.ResponseWriter, r *http.Request) {
	baseURL := getBaseURL(r)
	code := r.FormValue("code")
	if code == "" {
		http.Redirect(w, r, baseURL, http.StatusFound)
		return
	}

	token, err := getUserToken(code, baseURL, r.FormValue("state"))
	if err != nil {
		LogPages["Admin"].Println("getUserToken", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=TokenNotFound", http.StatusFound)
		return
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(r.Context(), ts)

	userData, err := getUserIds(tc)
	if err != nil {
		LogPages["Admin"].Println("getUserId", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=UserNotFound", http.StatusFound)
		return
	}

	tierTitle := ""
	invite, found := Config.Patreon.Emails[userData.Email]
	if found {
		tierTitle = invite
	}

	if tierTitle == "" {
		for _, userId := range userData.UserIds[1:] {
			foundTitle, _ := getUserTier(tc, userId)
			switch foundTitle {
			case "PIONEER", "PIONEER (Early Adopters)":
				tierTitle = "Pioneer"
			case "MODERN", "MODERN (Early Adopters)":
				tierTitle = "Modern"
			case "LEGACY", "LEGACY (Early Adopters)":
				tierTitle = "Legacy"
			case "Test Role":
				tierTitle = "Test Role"
			}
		}
	}

	if tierTitle == "" {
		LogPages["Admin"].Println("getUserTier returned an empty tier")
		http.Redirect(w, r, baseURL+"?errmsg=TierNotFound", http.StatusFound)
		return
	}

	LogPages["Admin"].Println(userData)
	LogPages["Admin"].Println(tierTitle)

	// Sign our base URL with our tier and other data
	sig := sign(baseURL, tierTitle, userData)

	// Keep it secret. Keep it safe.
	putSignatureInCookies(w, r, sig)

	// Reset path to be redirected to home page (it should just be /auth)
	r.URL.Path = ""
	// Clean up anything else (in particular remove "code")
	r.URL.RawQuery = ""

	// Redirect, we're done here
	http.Redirect(w, r, r.URL.String(), http.StatusFound)
}

func signHMACSHA1Base64(key []byte, data []byte) string {
	h := hmac.New(sha1.New, key)
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func getSignatureFromCookies(r *http.Request) string {
	var sig string
	for _, cookie := range r.Cookies() {
		if cookie.Name == "MTGBAN" {
			sig = cookie.Value
			break
		}
	}

	querySig := r.FormValue("sig")
	if sig == "" && querySig != "" {
		sig = querySig
	}

	exp := GetParamFromSig(sig, "Expires")
	if exp == "" {
		return ""
	}
	expires, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || expires < time.Now().Unix() {
		return ""
	}

	return sig
}

func putSignatureInCookies(w http.ResponseWriter, r *http.Request, sig string) {
	baseURL := getBaseURL(r)

	year, month, _ := time.Now().Date()
	endOfThisMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, time.Now().Location())
	domain := "mtgban.com"
	if strings.Contains(baseURL, "localhost") {
		domain = "localhost"
	}
	cookie := http.Cookie{
		Name:    "MTGBAN",
		Domain:  domain,
		Path:    "/",
		Expires: endOfThisMonth,
		Value:   sig,
	}

	http.SetCookie(w, &cookie)
}

// This function is mostly here only for initializing the host
// and the signature from invite links
func noSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic(r, w)

		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}

		querySig := r.FormValue("sig")
		if querySig != "" {
			putSignatureInCookies(w, r, querySig)
		}

		next.ServeHTTP(w, r)
	})
}

func enforceAPISigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic(r, w)

		w.Header().Add("RateLimit-Limit", fmt.Sprint(APIRequestsPerSec))

		ip, err := IpAddress(r)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if !APIRateLimiter.allow(string(ip)) {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		if !DatabaseLoaded {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}

		w.Header().Add("Content-Type", "application/json")

		sig := r.FormValue("sig")
		if SigCheck && sig == "" {
			log.Println("API error, empty signature")
			w.Write([]byte(`{"error": "empty signature"}`))
			return
		}

		raw, err := base64.StdEncoding.DecodeString(sig)
		if SigCheck && err != nil {
			log.Println("API error, no sig", err)
			w.Write([]byte(`{"error": "invalid signature"}`))
			return
		}

		v, err := url.ParseQuery(string(raw))
		if SigCheck && err != nil {
			log.Println("API error, no b64", err)
			w.Write([]byte(`{"error": "invalid b64 signature"}`))
			return
		}

		q := url.Values{}
		q.Set("API", v.Get("API"))

		for _, optional := range append(OrderNav, OptionalFields...) {
			val := v.Get(optional)
			if val != "" {
				q.Set(optional, val)
			}
		}

		sig = v.Get("Signature")
		exp := v.Get("Expires")

		secret := os.Getenv("BAN_SECRET")
		user_secret, found := Config.ApiUserSecrets[v.Get("UserEmail")]
		if found {
			secret = user_secret
		}

		data := fmt.Sprintf("%s%s%s%s", r.Method, exp, getBaseURL(r), q.Encode())
		valid := signHMACSHA1Base64([]byte(secret), []byte(data))
		expires, err := strconv.ParseInt(exp, 10, 64)
		if SigCheck && (err != nil || valid != sig || expires < time.Now().Unix()) {
			if DevMode {
				log.Println("API error, invalid", data)
			}
			w.Write([]byte(`{"error": "invalid or expired signature"}`))
			return
		}

		gziphandler.GzipHandler(next).ServeHTTP(w, r)
	})
}

func enforceSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic(r, w)

		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}
		sig := getSignatureFromCookies(r)
		querySig := r.FormValue("sig")
		if querySig != "" {
			sig = querySig
			putSignatureInCookies(w, r, querySig)
		}

		if r.Method != "GET" && r.URL.Path != "/upload" {
			http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		pageVars := genPageNav("Error", sig)

		if !UserRateLimiter.allow(GetParamFromSig(sig, "UserEmail")) && r.URL.Path != "/admin" {
			pageVars.Title = "Too Many Requests"
			pageVars.ErrorMessage = ErrMsgUseAPI

			render(w, "home.html", pageVars)
			return
		}

		raw, err := base64.StdEncoding.DecodeString(sig)
		if SigCheck && err != nil {
			pageVars.Title = "Unauthorized"
			pageVars.ErrorMessage = ErrMsg
			if DevMode {
				pageVars.ErrorMessage += " - " + err.Error()
			}

			render(w, "home.html", pageVars)
			return
		}

		v, err := url.ParseQuery(string(raw))
		if SigCheck && err != nil {
			pageVars.Title = "Unauthorized"
			pageVars.ErrorMessage = ErrMsg
			if DevMode {
				pageVars.ErrorMessage += " - " + err.Error()
			}

			render(w, "home.html", pageVars)
			return
		}

		q := url.Values{}
		for _, optional := range append(OrderNav, OptionalFields...) {
			val := v.Get(optional)
			if val != "" {
				q.Set(optional, val)
			}
		}

		expectedSig := v.Get("Signature")
		exp := v.Get("Expires")

		data := fmt.Sprintf("GET%s%s%s", exp, getBaseURL(r), q.Encode())
		valid := signHMACSHA1Base64([]byte(os.Getenv("BAN_SECRET")), []byte(data))
		expires, err := strconv.ParseInt(exp, 10, 64)
		if SigCheck && (err != nil || valid != expectedSig || expires < time.Now().Unix()) {
			if r.Method != "GET" {
				http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			pageVars.Title = "Unauthorized"
			pageVars.ErrorMessage = ErrMsg
			if valid == expectedSig && expires < time.Now().Unix() {
				pageVars.ErrorMessage = ErrMsgExpired
				pageVars.PatreonLogin = true
			}

			render(w, "home.html", pageVars)
			return
		}

		if !DatabaseLoaded {
			page := "home.html"
			for _, navName := range OrderNav {
				nav := ExtraNavs[navName]
				if r.URL.Path == nav.Link {
					pageVars = genPageNav(nav.Name, sig)
					page = nav.Page
				}
			}
			pageVars.Title = "Great things are coming"
			pageVars.ErrorMessage = ErrMsgRestart

			render(w, page, pageVars)
			return
		}

		for _, navName := range OrderNav {
			nav := ExtraNavs[navName]
			if r.URL.Path == nav.Link {
				param := GetParamFromSig(sig, navName)
				canDo, _ := strconv.ParseBool(param)
				if SigCheck && !canDo {
					pageVars = genPageNav(nav.Name, sig)
					pageVars.Title = "This feature is BANned"
					pageVars.ErrorMessage = ErrMsgPlus
					pageVars.ShowPromo = true

					render(w, nav.Page, pageVars)
					return
				}
				break
			}
		}

		gziphandler.GzipHandler(next).ServeHTTP(w, r)
	})
}

func recoverPanic(r *http.Request, w http.ResponseWriter) {
	errPanic := recover()
	if errPanic != nil {
		log.Println("panic occurred:", errPanic)

		// Restrict stack size to fit into discord message
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		if len(buf) > 1024 {
			buf = buf[:1024]
		}

		var msg string
		err, ok := errPanic.(error)
		if ok {
			msg = err.Error()
		} else {
			msg = "unknown error"
		}
		ServerNotify("panic", msg, true)
		ServerNotify("panic", string(buf))
		ServerNotify("panic", "source request: "+r.URL.String())

		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func getValuesForTier(tierTitle string) url.Values {
	v := url.Values{}
	// Enable option according to tier
	switch tierTitle {
	case "Root":
		v.Set("Explore", "true")
		fallthrough
	case "Admin":
		v.Set("Reverse", "true")
		v.Set("Admin", "true")
		fallthrough
	case "Developer", "Mods":
		fallthrough
	case "Test Role":
		v.Set("Arbit", "true")
		fallthrough
	case "Beta User":
		fallthrough
	case "Legacy":
		v.Set("Sleepers", "true")
		fallthrough
	case "Lost Boys":
		fallthrough
	case "Modern":
		v.Set("Newspaper", "true")
		v.Set("Global", "true")
		fallthrough
	case "Pioneer":
		v.Set("Upload", "true")
		v.Set("Search", "true")
	}
	if v.Get("Arbit") == "true" {
		switch tierTitle {
		case "Root":
			v.Set("ArbitEnabled", "ALL")
			v.Set("ArbitDisabledVendors", "NONE")
		case "Developer":
			v.Set("ArbitEnabled", "DEV")
			v.Set("ArbitDisabledVendors", "NONE")
		default:
			v.Set("ArbitEnabled", "DEFAULT")
			v.Set("ArbitDisabledVendors", "DEFAULT")
		}
	}
	if v.Get("Search") == "true" {
		switch tierTitle {
		case "Root", "Admin":
			v.Set("SearchDisabled", "NONE")
			v.Set("SearchBuylistDisabled", "NONE")
			v.Set("SearchSuper", "true")
			v.Set("SearchSealed", "true")
		case "Mods":
			v.Set("SearchSuper", "true")
			v.Set("SearchSealed", "true")
			fallthrough
		default:
			v.Set("SearchDisabled", "DEFAULT")
			v.Set("SearchBuylistDisabled", "DEFAULT")
		}
	}
	if v.Get("Explore") == "true" {
		switch tierTitle {
		case "Root":
			v.Set("ExpEnabled", "ALL")
		case "Admin":
			v.Set("ExpEnabled", "FULL")
		case "Test Role":
			v.Set("ExpEnabled", "MOST")
		case "Modern":
			v.Set("ExpEnabled", "ENTRY")
		case "Pioneer":
			v.Set("ExpEnabled", "DEMO")
		}
	}
	if v.Get("Newspaper") == "true" {
		switch tierTitle {
		case "Modern":
			v.Set("NewsEnabled", "3day")
		case "Root", "Admin":
			v.Set("NewsEnabled", "0day")
			v.Set("NewsBridgeEnabled", "true")
		default:
			v.Set("NewsEnabled", "1day")
		}
	}
	if v.Get("Global") == "true" {
		switch tierTitle {
		case "Modern":
			v.Set("AnyEnabled", "false")
		case "Root":
			v.Set("AnyExperimentsEnabled", "true")
			v.Set("AnyEnabled", "true")
		default:
			v.Set("AnyEnabled", "true")
		}
	}
	if v.Get("Upload") == "true" {
		switch tierTitle {
		case "Pioneer":
			v.Set("UploadBuylistEnabled", "false")
			v.Set("UploadChangeStoresEnabled", "false")
		case "Modern":
			v.Set("UploadBuylistEnabled", "false")
			v.Set("UploadChangeStoresEnabled", "true")
		case "Root", "Admin":
			v.Set("UploadOptimizer", "true")
			fallthrough
		default:
			v.Set("UploadBuylistEnabled", "true")
			v.Set("UploadChangeStoresEnabled", "true")
		}
	}
	return v
}

func sign(link string, tierTitle string, userData *PatreonUserData) string {
	v := getValuesForTier(tierTitle)
	if userData != nil {
		v.Set("UserName", userData.FullName)
		v.Set("UserEmail", userData.Email)
		v.Set("UserTier", tierTitle)
	}

	expires := time.Now().Add(DefaultSignatureDuration)
	data := fmt.Sprintf("GET%d%s%s", expires.Unix(), link, v.Encode())
	key := os.Getenv("BAN_SECRET")
	sig := signHMACSHA1Base64([]byte(key), []byte(data))

	v.Set("Expires", fmt.Sprintf("%d", expires.Unix()))
	v.Set("Signature", sig)
	str := base64.StdEncoding.EncodeToString([]byte(v.Encode()))

	return str
}

func GetParamFromSig(sig, param string) string {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return ""
	}
	v, err := url.ParseQuery(string(raw))
	if err != nil {
		return ""
	}
	return v.Get(param)
}

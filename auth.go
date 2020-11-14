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
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"golang.org/x/oauth2"
)

var PatreonHost string

const DefaultHost = "www.mtgban.com"

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

// Retrieve a user id for each membership of the current user
func getUserIds(tc *http.Client) ([]string, error) {
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

	log.Println(string(data))
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

	return userIds, nil
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
	log.Println(string(data))
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
		log.Println("getUserToken", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=TokenNotFound", http.StatusFound)
		return
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(r.Context(), ts)

	userIds, err := getUserIds(tc)
	if err != nil {
		log.Println("getUserId", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=UserNotFound", http.StatusFound)
		return
	}

	tierTitle := ""
	for _, tier := range []string{"root", "admin"} {
		if stringSliceContains(Config.Patreon.Ids[tier], userIds[0]) {
			tierTitle = strings.Title(tier)
		}
	}

	if tierTitle == "" {
		for _, userId := range userIds[1:] {
			foundTitle, _ := getUserTier(tc, userId)
			switch foundTitle {
			case "Squire",
				"Merchant",
				"Bard",
				"Test Role":
				tierTitle = foundTitle
			}
		}
	}

	if tierTitle == "" {
		log.Println("getUserTier returned an empty tier")
		http.Redirect(w, r, baseURL+"?errmsg=TierNotFound", http.StatusFound)
		return
	}

	log.Println(userIds, tierTitle)
	targetURL, sig := sign(tierTitle, r.URL, baseURL)

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

	// write the cookie to response
	http.SetCookie(w, &cookie)

	http.Redirect(w, r, targetURL, http.StatusFound)
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

	exp, err := GetParamFromSig(sig, "Expires")
	if err != nil {
		return ""
	}
	expires, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || expires < time.Now().Unix() {
		return ""
	}

	return sig
}

// This function is mostly here only for initializing the host
func noSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}

		if r.FormValue("sig") == "" && r.FormValue("errmsg") == "" {
			sig := getSignatureFromCookies(r)
			if sig != "" {
				form, _ := url.ParseQuery(r.URL.RawQuery)
				form.Add("sig", sig)
				r.URL.RawQuery = form.Encode()
				http.Redirect(w, r, r.URL.String(), http.StatusSeeOther)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func enforceAPISigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.FormValue("sig")
		if sig == "" {
			log.Println("API error, empty signature")
			http.NotFound(w, r)
			return
		}

		raw, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			log.Println("API error, no sig", err)
			http.NotFound(w, r)
			return
		}

		v, err := url.ParseQuery(string(raw))
		if SigCheck && err != nil {
			log.Println("API error, no b64", err)
			http.NotFound(w, r)
			return
		}

		q := url.Values{}
		q.Set("API", v.Get("API"))

		sig = v.Get("Signature")
		exp := v.Get("Expires")

		data := fmt.Sprintf("%s%s%s%s", r.Method, exp, getBaseURL(r), q.Encode())
		valid := signHMACSHA1Base64([]byte(os.Getenv("BAN_SECRET")), []byte(data))
		expires, err := strconv.ParseInt(exp, 10, 64)
		if err != nil || valid != sig || expires < time.Now().Unix() {
			log.Println("API error, invalid")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		gziphandler.GzipHandler(next).ServeHTTP(w, r)
	})
}

func enforceSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}
		sig := r.FormValue("sig")

		if r.FormValue("sig") == "" {
			sig := getSignatureFromCookies(r)
			if sig != "" {
				form, _ := url.ParseQuery(r.URL.RawQuery)
				form.Add("sig", sig)
				r.URL.RawQuery = form.Encode()
				http.Redirect(w, r, r.URL.String(), http.StatusSeeOther)
				return
			}
		}

		pageVars := genPageNav("Error", sig)

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

		sig = v.Get("Signature")
		exp := v.Get("Expires")

		data := fmt.Sprintf("%s%s%s%s", r.Method, exp, getBaseURL(r), q.Encode())
		valid := signHMACSHA1Base64([]byte(os.Getenv("BAN_SECRET")), []byte(data))
		expires, err := strconv.ParseInt(exp, 10, 64)
		if SigCheck && (err != nil || valid != sig || expires < time.Now().Unix()) {
			pageVars.Title = "Unauthorized"
			pageVars.ErrorMessage = ErrMsg
			if valid == sig && expires < time.Now().Unix() {
				pageVars.ErrorMessage = ErrMsgExpired
				pageVars.PatreonLogin = true
			}

			render(w, "home.html", pageVars)
			return
		}

		gziphandler.GzipHandler(next).ServeHTTP(w, r)
	})
}

func sign(tierTitle string, sourceURL *url.URL, baseURL string) (string, string) {
	duration := 10 * 24 * time.Hour
	expires := time.Now().Add(duration)

	v := url.Values{}
	// Enable option according to tier
	switch tierTitle {
	case "Squire":
		v.Set("Search", "true")
	case "Merchant":
		v.Set("Search", "true")
		v.Set("Newspaper", "true")
	case "Bard":
		v.Set("Search", "true")
		v.Set("Newspaper", "true")
		v.Set("Sleepers", "true")
	case "Test Role":
		v.Set("Search", "true")
		v.Set("Newspaper", "true")
		v.Set("Sleepers", "true")
		v.Set("Arbit", "true")
	case "Root":
		v.Set("Explore", "true")
		fallthrough
	case "Admin":
		v.Set("Search", "true")
		v.Set("Newspaper", "true")
		v.Set("Sleepers", "true")
		v.Set("Arbit", "true")
	}
	if v.Get("Arbit") == "true" {
		if tierTitle == "Root" {
			var allowlistSellers []string
			for i, seller := range Sellers {
				if seller == nil {
					log.Println("nil seller at position", i)
					continue
				}
				allowlistSellers = append(allowlistSellers, seller.Info().Shorthand)
			}
			v.Set("ArbitEnabled", strings.Join(allowlistSellers, ","))
			v.Set("ArbitDisabledVendors", "NONE")
		} else {
			v.Set("ArbitEnabled", strings.Join(Config.ArbitDefaultSellers, ","))
			v.Set("ArbitDisabledVendors", strings.Join(Config.ArbitBlockVendors, ","))
		}
	}
	if v.Get("Search") == "true" {
		if tierTitle == "Root" {
			v.Set("SearchDisabled", "NONE")
		} else if tierTitle == "Admin" {
			var allowlistSellers []string
			for _, sellerName := range Config.SearchBlockList {
				if strings.Contains(sellerName, "MKM") {
					continue
				}
				allowlistSellers = append(allowlistSellers, sellerName)
			}

			v.Set("SearchDisabled", strings.Join(allowlistSellers, ","))
		} else {
			v.Set("SearchDisabled", strings.Join(Config.SearchBlockList, ","))
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
		case "Merchant":
			v.Set("ExpEnabled", "ENTRY")
		case "Squire":
			v.Set("ExpEnabled", "DEMO")
		}
	}
	if v.Get("Newspaper") == "true" {
		if tierTitle == "Merchant" {
			v.Set("NewsEnabled", "3day")
		} else {
			v.Set("NewsEnabled", "1day")
		}
	}

	bu, _ := url.Parse(baseURL)
	sourceURL.Scheme = bu.Scheme
	sourceURL.Host = bu.Host

	data := fmt.Sprintf("GET%d%s%s", expires.Unix(), sourceURL.Scheme+"://"+sourceURL.Host, v.Encode())
	key := os.Getenv("BAN_SECRET")
	sig := signHMACSHA1Base64([]byte(key), []byte(data))

	v.Set("Expires", fmt.Sprintf("%d", expires.Unix()))
	v.Set("Signature", sig)
	str := base64.StdEncoding.EncodeToString([]byte(v.Encode()))

	q := sourceURL.Query()
	q.Del("code")
	q.Set("sig", str)
	sourceURL.RawQuery = q.Encode()
	sourceURL.Path = ""

	return sourceURL.String(), str
}

func GetParamFromSig(sig, param string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return "", err
	}
	v, err := url.ParseQuery(string(raw))
	if err != nil {
		return "", err
	}
	return v.Get(param), nil
}

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

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"golang.org/x/oauth2"
)

var PatreonHost string

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
	ErrMsgExpired = "You've been logged out"
)

func getUserToken(code, baseURL string) (string, error) {
	resp, err := cleanhttp.DefaultClient().PostForm(PatreonTokenURL, url.Values{
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"client_id":     {PatreonClientId},
		"client_secret": {os.Getenv("PATREON_SECRET")},
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

func getUserId(tc *http.Client) (string, error) {
	resp, err := tc.Get(PatreonIdentityURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
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
				} `json:memberships"`
			} `json:"relationships"`
			IdV1 string `json:"id"`
		} `json:"data"`
	}

	err = json.Unmarshal(data, &userData)
	if err != nil {
		return "", err
	}
	if len(userData.Errors) > 0 {
		return "", errors.New(userData.Errors[0].CodeName)
	}

	userId := ""
	for _, memberData := range userData.Data.Relationships.Memberships.Data {
		if memberData.Type == "member" {
			userId = memberData.Id
			break
		}
	}
	if userId == "" {
		userId = userData.Data.IdV1
	}
	if userId == "" {
		return "", errors.New("empty user id")
	}

	return userId, nil
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
	if host == "mtgban.herokuapp.com" {
		host = "www.mtgban.com"
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
		log.Println("Empty auth code query param")
		u, _ := url.Parse(PatreonHost)
		http.Redirect(w, r, u.Scheme+u.Host, http.StatusFound)
		return
	}

	token, err := getUserToken(code, baseURL)
	if err != nil {
		log.Println("getUserToken", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=TokenNotFound", http.StatusFound)
		return
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(r.Context(), ts)

	userId, err := getUserId(tc)
	if err != nil {
		log.Println("getUserId", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=UserNotFound", http.StatusFound)
		return
	}

	tierTitle := ""
	switch userId {
	case "26313002":
		tierTitle = "Root"
	case "28316283":
		tierTitle = "Admin"
	default:
		tierTitle, err = getUserTier(tc, userId)
		if err != nil {
			log.Println("getUserTier", err.Error())
			http.Redirect(w, r, baseURL+"?errmsg=TierNotFound", http.StatusFound)
			return
		}
	}

	targetURL := sign(tierTitle, r.URL, baseURL)

	http.Redirect(w, r, targetURL, http.StatusFound)
}

func signHMACSHA1Base64(key []byte, data []byte) string {
	h := hmac.New(sha1.New, key)
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// This function is mostly here only for initializing the host
func noSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}
		next.ServeHTTP(w, r)
	})
}

func enforceSigning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PatreonHost == "" {
			PatreonHost = getBaseURL(r) + "/auth"
		}
		sign := r.FormValue("sig")

		pageVars := genPageNav("Error", sign)

		raw, err := base64.StdEncoding.DecodeString(sign)
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
		for _, param := range []string{"Search", "Arbit"} {
			q.Set(param, v.Get(param))
		}
		optionalEnabled := v.Get("Enabled")
		if optionalEnabled != "" {
			q.Set("Enabled", optionalEnabled)
		}

		sig := v.Get("Signature")
		exp := v.Get("Expires")

		data := fmt.Sprintf("%s%s%s%s", r.Method, exp, r.Host, q.Encode())
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

		next.ServeHTTP(w, r)
	})
}

func sign(tierTitle string, sourceURL *url.URL, baseURL string) string {
	duration := 3 * time.Hour
	expires := time.Now().Add(duration)

	v := url.Values{}
	switch tierTitle {
	case "Squire":
	case "Merchant":
		v.Set("Search", "true")
		v.Set("Arbit", "false")
	case "Knight", "Admin", "Root":
		v.Set("Search", "true")
		v.Set("Arbit", "true")
		if tierTitle == "Root" {
			v.Set("Enabled", "ALL")
		} else {
			v.Set("Enabled", "DEFAULT")
		}
	}

	bu, _ := url.Parse(baseURL)
	sourceURL.Scheme = bu.Scheme
	sourceURL.Host = bu.Host

	data := fmt.Sprintf("GET%d%s%s", expires.Unix(), sourceURL.Host, v.Encode())
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

	return sourceURL.String()
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

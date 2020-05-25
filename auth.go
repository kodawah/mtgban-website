package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"golang.org/x/oauth2"
)

const (
	PatreonClientId = "VrjStFvhtp7HhF1xItHm83FMY7PK3nptpls1xVkYL5IDufXNVW4Xb-pHPXBIuWZ4"

	PatreonTokenURL    = "https://www.patreon.com/api/oauth2/token"
	PatreonIdentityURL = "https://www.patreon.com/api/oauth2/v2/identity?include=memberships&fields%5Buser%5D=email,first_name,full_name,image_url,last_name,social_connections,thumb_url,url,vanity"
	PatreonMemberURL   = "https://www.patreon.com/api/oauth2/v2/members/"
	PatreonMemberOpts  = "?include=currently_entitled_tiers&fields%5Btier%5D=title"
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

func getBaseURL(r *http.Request) string {
	baseURL := "http://" + r.Host
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
		http.Redirect(w, r, baseURL, http.StatusFound)
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

	tierTitle, err := getUserTier(tc, userId)
	if err != nil {
		log.Println("getUserTier", err.Error())
		http.Redirect(w, r, baseURL+"?errmsg=TierNotFound", http.StatusFound)
		return
	}

	targetURL := sign(tierTitle, r.URL, baseURL)

	http.Redirect(w, r, targetURL, http.StatusFound)
}

func sign(tierTitle string, sourceURL *url.URL, baseURL string) string {
	duration := 7 * 24 * time.Hour
	expires := time.Now().Add(duration)

	v := url.Values{}
	switch tierTitle {
	case "Squire":
	case "Merchant":
		v.Set("Search", "true")
		v.Set("Arbit", "false")
	case "Knight":
		v.Set("Search", "true")
		v.Set("Arbit", "true")
		v.Set("Enabled", "DEFAULT")
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

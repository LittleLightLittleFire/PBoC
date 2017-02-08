package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
)

// BotConfig store the bot configuration.
// Read https://www.cs.cmu.edu/~lingwang/weiboguide/ to get set up
// Create an app and authorize the app on your behalf, just like Twitter
type BotConfig struct {
	WeiboAppKey         string `json:"weibo_app_key"`
	WeiboAppSecret      string `json:"weibo_app_secret"`
	WeiboConsumerSecret string `json:"weibo_consumer_secret"`
	WeiboRedirectURL    string `json:"weibo_redirect_url"`
}

func loadConfig() (config BotConfig, err error) {
	configPath := os.Getenv("CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}

	file, err := os.Open(configPath)
	if err != nil {
		return config, err
	}

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return config, err
	}

	return config, nil
}

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("Unable to load config:", err)
	}

	// Retrieve the access token from Weibo
	resp, err := http.PostForm("https://api.weibo.com/oauth2/access_token", url.Values(map[string][]string{
		"client_id":     []string{cfg.WeiboAppKey},
		"client_secret": []string{cfg.WeiboAppSecret},
		"redirect_uri":  []string{cfg.WeiboRedirectURL},
		"grant_type":    []string{"authorization_code"},
		"code":          []string{cfg.WeiboConsumerSecret},
	}))
	if err != nil {
		log.Fatal("Failed to authenticate to weibo:", err)
	}
	defer resp.Body.Close()

	var token struct {
		AccessToken string `json:"access_token"`
		RemindIn    int64  `json:"remind_in"`
		ExpiresIn   int64  `json:"expires_in"`
		UID         int64  `json:"uid"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		log.Fatal("Failed to retrieve body:", err)
	}

	log.Println(token)
}

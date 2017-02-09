package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/hashicorp/errwrap"
)

// BotConfig store the bot configuration.
// Read https://www.cs.cmu.edu/~lingwang/weiboguide/ to get set up
// Create an app and authorize the app on your behalf, just like Twitter
// We'll use our account as the feed, and just read our account.
// The API does not allow us to read statuses of other users.
type BotConfig struct {
	WeiboAccessToken      string `json:"weibo_access_token"`
	TwitterConsumerKey    string `json:"twitter_consumer_key"`
	TwitterConsumerSecret string `json:"twitter_consumer_secret"`
	TwitterAccessToken    string `json:"twitter_access_token"`
	TwitterTokenSecret    string `json:"twitter_token_secret"`
}

var cfg BotConfig

// Status defines a Weibo status.
type Status struct {
	User struct {
		Name       string `json:"name"`
		ScreenName string `json:"screen_name"`
	} `json:"user"`
	ID           int64  `json:"id"`
	RawCreatedAt string `json:"created_at"`
	Text         string `json:"text"`

	CreatedAt time.Time `json:"-"`
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

func fetchJSON(url string, v interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return errwrap.Wrapf("failed to GET: {{err}}", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return errwrap.Wrapf("failed to failed to decode body: {{err}}", err)
	}

	return nil
}

func fetchStatus(sinceID int64) ([]Status, error) {
	var url = url.URL{
		Scheme: "https",
		Host:   "api.weibo.com",
		Path:   "/2/statuses/home_timeline.json",
		RawQuery: url.Values(map[string][]string{
			"access_token": []string{cfg.WeiboAccessToken},
			"since_id":     []string{fmt.Sprint(sinceID)},
			"max_id":       []string{"0"},
			"count":        []string{"100"},
			"page":         []string{"1"},
			"base_app":     []string{"0"},
			"feature":      []string{"0"},
			"trim_user":    []string{"0"},
		}).Encode(),
	}

	var timeline struct {
		Statuses []Status `json:"statuses"`
	}

	if err := fetchJSON(url.String(), &timeline); err != nil {
		return nil, errwrap.Wrapf("failed to fetch status: {{err}}", err)
	}

	// Convert the time on the status
	for i := range timeline.Statuses {
		var err error
		timeline.Statuses[i].CreatedAt, err = time.Parse("Mon Jan 2 15:04:05 -0700 2006", timeline.Statuses[i].RawCreatedAt)
		if err != nil {
			return nil, errwrap.Wrapf("failed to convert time: {{err}}", err)
		}
	}

	return timeline.Statuses, nil
}

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags | log.Lmicroseconds)

	var err error
	if cfg, err = loadConfig(); err != nil {
		log.Fatal("Unable to load config:", err)
	}

	// Login to Twitter
	client := twitter.NewClient(oauth1.NewConfig(cfg.TwitterConsumerKey, cfg.TwitterConsumerSecret).Client(oauth1.NoContext, oauth1.NewToken(cfg.TwitterAccessToken, cfg.TwitterTokenSecret)))
	user, _, err := client.Accounts.VerifyCredentials(nil)
	if err != nil {
		log.Fatal("Failed to verify Twitter credentials:", err)
	}

	log.Println("Logged in as:", user.Name)

	// Fetch the initial status to get the ID
	statuses, err := fetchStatus(0)
	if err != nil {
		log.Println("Error fetching weibo:", err)
	}

	var start int64
	if len(statuses) > 0 {
		start = statuses[0].ID
	}
	log.Println("Initial ID set:", start)

	for {
		statuses, err = fetchStatus(start)
		if err != nil {
			log.Println("Error fetching weibo:", err)
		}

		for _, status := range statuses {
			// Report all bitcoin related statuses we'll get some false positives from translated sources but it is useful to test
			bitcoin := "比特币" // bitcoin

			if strings.Contains(status.Text, bitcoin) {
				// Generate the tweet
				runes := ([]rune)(fmt.Sprintf("%v: %v", status.User.Name, status.Text))
				if len(runes) > 140 {
					runes = []rune(string(runes[:140-4]) + " ...")
				}

				// Send the tweet
				if tweet, _, err := client.Statuses.Update(string(runes), nil); err != nil {
					log.Println("Failed to tweet:", status)
				} else {
					log.Printf("Sent tweet: %v: '%v'\n", tweet.IDStr, status)
				}
			}
		}

		if len(statuses) > 0 {
			start = statuses[0].ID
			log.Println("Last ID:", start)
		}

		time.Sleep(30 * time.Second)
	}
}

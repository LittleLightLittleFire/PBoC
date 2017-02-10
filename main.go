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
var httpClient = http.Client{
	Timeout: 10 * time.Second,
}

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
	resp, err := httpClient.Get(url)
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
		Error    string   `json:"error"`
	}

	if err := fetchJSON(url.String(), &timeline); err != nil {
		return nil, errwrap.Wrapf("failed to fetch status: {{err}}", err)
	}

	if timeline.Error != "" {
		return nil, fmt.Errorf("failed to fetch status: %v", timeline.Error)
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

	// Load China timezone so we can throttle our requests
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Fatal("Failed to load CST time:", err)
	}

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
		} else {
			log.Println("Loaded:", len(statuses), "statuses")
		}

		for _, status := range statuses {
			// Check the source of the status update
			if strings.Contains(status.User.ScreenName, "火币网") ||
				strings.Contains(status.User.ScreenName, "OKCoin") ||
				strings.Contains(status.User.ScreenName, "YourBTCC") {
				// If the source is an exchange, filter the fluff

				if !(strings.Contains(status.Text, "公告") || // Announcement
					strings.Contains(status.Text, "尊敬") || // "Dear"
					strings.Contains(status.Text, "用户")) { // "Customer"
					continue
				}
			} else {
				// News report, must contain the keyword bitcoin
				if !strings.Contains(status.Text, "比特币") { // bitcoin
					continue
				}
			}

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

		if len(statuses) > 0 {
			start = statuses[0].ID
			log.Println("Last ID:", start)
		}

		// Reduce fetch time during periods of low activity, Weibo's per day request quota is very low
		// They keeps banning us: they allow ~470 requests a day
		beijingTime := time.Now().In(loc)
		if beijingTime.Hour() >= 7 && beijingTime.Hour() <= 19 { // 8:00 to 18:00 are the work hours
			time.Sleep(3 * 60 * time.Second)
		} else {
			time.Sleep(5 * 60 * time.Second)
		}
	}
}

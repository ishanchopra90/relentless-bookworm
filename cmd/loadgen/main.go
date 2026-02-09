package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// Config holds the seeds to submit to the API.
type Config struct {
	Seeds []string `json:"seeds"`
}

func main() {
	configPath := flag.String("config", "seeds.json", "Path to JSON config file with seeds")
	apiBase := flag.String("api", "http://localhost:30080", "API base URL (nodePort when hitting Kind from host; e.g. http://localhost:30080)")
	flag.Parse()

	if err := run(*configPath, *apiBase, nil); err != nil {
		log.Fatal(err)
	}
}

// run loads config from configPath, parses apiBase, and submits all seeds to the API concurrently.
// If client is nil, a default HTTP client (30s timeout) is used.
func run(configPath, apiBase string, client *http.Client) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	baseURL, err := url.Parse(apiBase)
	if err != nil {
		return err
	}

	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var wg sync.WaitGroup
	for i, seed := range cfg.Seeds {
		wg.Add(1)
		go func(idx int, s string) {
			defer wg.Done()
			submitSeed(client, baseURL, idx, s)
		}(i, seed)
	}
	wg.Wait()
	log.Printf("submitted %d seeds", len(cfg.Seeds))
	return nil
}

// loadConfig reads and parses the JSON config file.
func loadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if len(cfg.Seeds) == 0 {
		return cfg, errNoSeeds
	}
	return cfg, nil
}

var errNoSeeds = fmt.Errorf("config has no seeds")

func submitSeed(client *http.Client, base *url.URL, idx int, seed string) {
	u := *base
	u.Path = "/crawl"
	u.RawQuery = url.Values{"url": {seed}}.Encode()

	resp, err := client.Post(u.String(), "", nil)
	if err != nil {
		log.Printf("[%d] seed=%q err=%v", idx, seed, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		log.Printf("[%d] seed=%q status=%d", idx, seed, resp.StatusCode)
		return
	}
	log.Printf("[%d] seed=%q accepted", idx, seed)
}

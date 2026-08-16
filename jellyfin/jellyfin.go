package jellyfin

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Address  string
	User     string
	Password string
	Insecure bool

	token    string
	userID   string
	mu       sync.Mutex
	client   *http.Client
}

type authResponse struct {
	AccessToken string `json:"AccessToken"`
	User        struct {
		ID string `json:"Id"`
	} `json:"User"`
}

type searchResponse struct {
	Items []SearchItem `json:"Items"`
}

type SearchItem struct {
	ID                     string            `json:"Id"`
	Name                   string            `json:"Name"`
	Type                   string            `json:"Type"`
	AlbumArtist            string            `json:"AlbumArtist"`
	Album                  string            `json:"Album"`
	Year                   int               `json:"Year"`
	IndexNumber            int               `json:"IndexNumber"`
	RunTimeTicks           int64             `json:"RunTimeTicks"`
	PrimaryImageTag        string            `json:"PrimaryImageTag"`
	AlbumPrimaryImageTag   string            `json:"AlbumPrimaryImageTag"`
	AlbumId                string            `json:"AlbumId"`
	ImageTags              map[string]string `json:"ImageTags"`
}

type Track struct {
	ID        string
	Name      string
	Title     string
	Artist    string
	Album     string
	Year      int
	TrackNum  int
	Duration  time.Duration
	URL       string
	Thumbnail string
}

func NewClient(address, user, password string, insecure bool) *Client {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		Address:  strings.TrimRight(address, "/"),
		User:     user,
		Password: password,
		Insecure: insecure,
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}
}

func (c *Client) Authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	authURL := c.Address + "/Users/AuthenticateByName"
	body, _ := json.Marshal(map[string]string{
		"Username": c.User,
		"Pw":       c.Password,
	})

	req, err := http.NewRequest("POST", authURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Panoptic", Device="Go", DeviceId="panoptic-bot", Version="1.0.0"`)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth HTTP failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth failed with status %d", resp.StatusCode)
	}

	var authResp authResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("auth decode failed: %w", err)
	}

	c.token = authResp.AccessToken
	c.userID = authResp.User.ID
	log.Printf("[Jellyfin] Authenticated to %s as %s", c.Address, c.User)
	return nil
}

func (c *Client) ensureAuth() error {
	if c.token == "" {
		return c.Authenticate()
	}
	return nil
}

func (c *Client) doRequest(method, path string, params url.Values) ([]byte, error) {
	if err := c.ensureAuth(); err != nil {
		return nil, err
	}

	reqURL := c.Address + path
	if params != nil {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Emby-Token", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		c.token = ""
		if err := c.Authenticate(); err != nil {
			return nil, err
		}
		req.Header.Set("X-Emby-Token", c.token)
		resp, err = c.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) Search(query string, limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 5
	}

	params := url.Values{}
	params.Set("searchTerm", query)
	params.Set("Recursive", "true")
	params.Set("IncludeItemTypes", "Audio")
	params.Set("Limit", fmt.Sprintf("%d", limit*2))
	params.Set("Fields", "Overview,PrimaryImageAspectRatio,ProductionYear,AlbumArtist,Album,IndexNumber")

	data, err := c.doRequest("GET", fmt.Sprintf("/Users/%s/Items", c.userID), params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var searchResp searchResponse
	if err := json.Unmarshal(data, &searchResp); err != nil {
		return nil, fmt.Errorf("search decode failed: %w", err)
	}

	tracks := make([]Track, 0)
	for _, item := range searchResp.Items {
		if item.Type != "Audio" {
			continue
		}

		track := Track{
			ID:       item.ID,
			Name:     item.Name,
			Artist:   item.AlbumArtist,
			Album:    item.Album,
			Year:     item.Year,
			TrackNum: item.IndexNumber,
			Duration: time.Duration(item.RunTimeTicks) * 100 * time.Nanosecond,
			URL:      fmt.Sprintf("%s/Items/%s/Download?api_key=%s", c.Address, item.ID, c.token),
		}

		imgTag := item.PrimaryImageTag
		if imgTag == "" {
			if tag, ok := item.ImageTags["Primary"]; ok {
				imgTag = tag
			}
		}
		if imgTag != "" {
			track.Thumbnail = fmt.Sprintf("%s/Items/%s/Images/Primary", c.Address, item.ID)
		} else if item.AlbumPrimaryImageTag != "" && item.AlbumId != "" {
			track.Thumbnail = fmt.Sprintf("%s/Items/%s/Images/Primary", c.Address, item.AlbumId)
		}

		track.Title = formatTrackTitle(track)
		tracks = append(tracks, track)
	}

	return tracks, nil
}

func formatTrackTitle(t Track) string {
	parts := make([]string, 0)
	if t.Artist != "" {
		parts = append(parts, t.Artist)
	}
	if t.Album != "" {
		parts = append(parts, t.Album)
	}
	if t.Name != "" {
		parts = append(parts, t.Name)
	}
	return strings.Join(parts, " - ")
}

func (c *Client) GetToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *Client) GetItemThumbnailURL(itemID string) string {
	return fmt.Sprintf("%s/Items/%s/Images/Primary?maxWidth=100&maxHeight=100", c.Address, itemID)
}

func (c *Client) GetItemDownloadURL(itemID string) string {
	return fmt.Sprintf("%s/Items/%s/Download?api_key=%s", c.Address, itemID, c.token)
}

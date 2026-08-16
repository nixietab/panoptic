package source

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nixietab/panoptic/utils"
	"layeh.com/gumble/gumbleffmpeg"
)

var radioBrowserBase = "https://de1.api.radio-browser.info/json"

var radioBrowserBases = []string{
	"https://de1.api.radio-browser.info/json",
	"https://nl1.api.radio-browser.info/json",
	"https://at1.api.radio-browser.info/json",
}

type RadioStation struct {
	Name        string  `json:"name"`
	URLResolved string  `json:"url_resolved"`
	LogoURL     string  `json:"favicon"`
	Tags        string  `json:"tags"`
	Country     string  `json:"country"`
	Codec       string  `json:"codec"`
	Bitrate     int     `json:"bitrate"`
	Homepage    string  `json:"homepage"`
	Votes       int     `json:"votes"`
}

// RadioCache is a locally stored copy of the full station list so searches
// keep working when the radio-browser API is unreachable.
type RadioCache struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Stations  []RadioStation `json:"stations"`
}

type RadioBrowserClient struct {
	client *http.Client

	mu                sync.RWMutex
	cache             *RadioCache
	cacheFile         string
	cacheTTL          time.Duration
	cacheInMemory     bool
	cacheFetchedAt    time.Time
	cacheStationCount int
	refreshing        bool
}

func NewRadioBrowserClient() *RadioBrowserClient {
	return &RadioBrowserClient{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (r *RadioBrowserClient) SearchByName(query string, limit int) []RadioStation {
	r.mu.RLock()
	cache := r.cache
	inMemory := r.cacheInMemory
	file := r.cacheFile
	r.mu.RUnlock()

	if cache != nil && inMemory {
		r.refreshIfStale()
		return cache.SearchByName(query, limit)
	}

	if file != "" {
		r.refreshIfStale()
		if matches, ok := searchStationsFile(file, query, limit, false); ok {
			return matches
		}
	}

	return r.searchByNameNetwork(query, limit)
}

func (r *RadioBrowserClient) searchByNameNetwork(query string, limit int) []RadioStation {
	if limit <= 0 {
		limit = 5
	}

	encoded := url.PathEscape(query)
	apiURL := fmt.Sprintf("%s/stations/byname/%s?limit=%d&order=votes&reverse=true", radioBrowserBase, encoded, limit)

	log.Printf("[RadioBrowser] Searching: %s", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("[RadioBrowser] Request failed: %v", err)
		return nil
	}
	req.Header.Set("User-Agent", "Panoptic/1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		log.Printf("[RadioBrowser] HTTP failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RadioBrowser] HTTP %d", resp.StatusCode)
		return nil
	}

	var stations []RadioStation
	if err := decodeJSON(resp.Body, &stations); err != nil {
		log.Printf("[RadioBrowser] JSON parse failed: %v", err)
		return nil
	}

	log.Printf("[RadioBrowser] Found %d station(s)", len(stations))
	return stations
}

func (r *RadioBrowserClient) SearchByTag(query string, limit int) []RadioStation {
	r.mu.RLock()
	cache := r.cache
	inMemory := r.cacheInMemory
	file := r.cacheFile
	r.mu.RUnlock()

	if cache != nil && inMemory {
		r.refreshIfStale()
		return cache.SearchByTag(query, limit)
	}

	if file != "" {
		r.refreshIfStale()
		if matches, ok := searchStationsFile(file, query, limit, true); ok {
			return matches
		}
	}

	return r.searchByTagNetwork(query, limit)
}

func (r *RadioBrowserClient) searchByTagNetwork(query string, limit int) []RadioStation {
	if limit <= 0 {
		limit = 5
	}

	encoded := url.PathEscape(query)
	apiURL := fmt.Sprintf("%s/stations/bytag/%s?limit=%d&order=votes&reverse=true", radioBrowserBase, encoded, limit)

	log.Printf("[RadioBrowser] Searching by tag: %s", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("[RadioBrowser] Request failed: %v", err)
		return nil
	}
	req.Header.Set("User-Agent", "Panoptic/1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		log.Printf("[RadioBrowser] HTTP failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RadioBrowser] HTTP %d", resp.StatusCode)
		return nil
	}

	var stations []RadioStation
	if err := decodeJSON(resp.Body, &stations); err != nil {
		log.Printf("[RadioBrowser] JSON parse failed: %v", err)
		return nil
	}

	log.Printf("[RadioBrowser] Found %d station(s) by tag", len(stations))
	return stations
}

// LoadCache loads the locally cached station list. If the file is missing or
// older than ttlHours it refreshes the list from radio-browser. A refresh
// failure falls back to the stale cache so searches keep working offline.
//
// When inMemory is false the station list is NOT kept in RAM; searches stream
// the JSON from disk on demand instead, trading a bit of I/O for memory.
func (r *RadioBrowserClient) LoadCache(file string, ttlHours int, inMemory bool) error {
	if file == "" {
		file = "radio_stations.json"
	}

	r.mu.Lock()
	r.cacheFile = file
	r.cacheTTL = time.Duration(ttlHours) * time.Hour
	r.cacheInMemory = inMemory
	r.mu.Unlock()

	if !inMemory {
		fetchedAt, count, err := loadRadioCacheMeta(file)
		if err != nil {
			log.Printf("[Radio] No usable station cache (%s), fetching fresh list...", file)
			r.refreshCache()
			return nil
		}

		r.mu.Lock()
		r.cacheFetchedAt = fetchedAt
		r.cacheStationCount = count
		r.mu.Unlock()

		if r.cacheTTL > 0 && time.Since(fetchedAt) > r.cacheTTL {
			log.Printf("[Radio] Station cache is stale (%s old, TTL %s), refreshing...", formatCacheAge(fetchedAt), r.cacheTTL)
			r.refreshCache()
		} else {
			log.Printf("[Radio] Using cached station list (streamed from disk): %d stations (%s old)", count, formatCacheAge(fetchedAt))
		}
		return nil
	}

	cache, err := loadRadioCache(file)
	if err != nil {
		r.refreshCache()
		if r.getCache() == nil {
			return fmt.Errorf("no station cache (%s) and initial fetch failed", file)
		}
		return nil
	}

	r.setCache(cache)
	if cache.isFresh(r.cacheTTL) {
		log.Printf("[Radio] Using cached station list: %d stations (%s old)", len(cache.Stations), formatCacheAge(cache.FetchedAt))
		return nil
	}

	log.Printf("[Radio] Station cache is stale (%s old, TTL %s), refreshing...", formatCacheAge(cache.FetchedAt), r.cacheTTL)
	r.refreshCache()
	return nil
}

// refreshIfStale triggers a background refresh of an expired cache. It is safe
// to call from multiple goroutines; only one refresh runs at a time.
func (r *RadioBrowserClient) refreshIfStale() {
	r.mu.RLock()
	cache := r.cache
	fetchedAt := r.cacheFetchedAt
	ttl := r.cacheTTL
	r.mu.RUnlock()

	stale := false
	if cache != nil {
		stale = !cache.isFresh(ttl)
	} else if !fetchedAt.IsZero() {
		stale = ttl > 0 && time.Since(fetchedAt) > ttl
	}

	if stale {
		go r.refreshCache()
	}
}

func (r *RadioBrowserClient) refreshCache() {
	r.mu.Lock()
	if r.refreshing {
		r.mu.Unlock()
		return
	}
	r.refreshing = true
	file := r.cacheFile
	inMemory := r.cacheInMemory
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.refreshing = false
		r.mu.Unlock()
	}()

	log.Println("[Radio] Fetching full station list from radio-browser...")

	if !inMemory {
		total, err := r.streamAllStationsToFile(file)
		if err != nil {
			log.Printf("[Radio] Station list refresh failed: %v", err)
			return
		}
		r.mu.Lock()
		r.cacheFetchedAt = time.Now()
		r.cacheStationCount = total
		r.mu.Unlock()
		log.Printf("[Radio] Station cache refreshed and saved (streamed to disk): %d stations", total)
		return
	}

	stations, err := r.fetchAllStations()
	if err != nil {
		log.Printf("[Radio] Station list refresh failed: %v", err)
		return
	}

	cache := &RadioCache{FetchedAt: time.Now(), Stations: stations}
	if err := saveRadioCache(file, cache); err != nil {
		log.Printf("[Radio] Failed to save station cache %s: %v", file, err)
	}
	r.setCache(cache)
	log.Printf("[Radio] Station cache refreshed and saved: %d stations", len(stations))
}

// fetchAllStations downloads the full station list, paging over the API and
// falling back across mirror servers when one is unreachable.
func (r *RadioBrowserClient) fetchAllStations() ([]RadioStation, error) {
	const pageSize = 10000
	const maxPages = 30

	for _, base := range radioBrowserBases {
		all := make([]RadioStation, 0, pageSize)
		for page := 0; page < maxPages; page++ {
			apiURL := fmt.Sprintf("%s/stations?offset=%d&limit=%d", base, page*pageSize, pageSize)

			req, err := http.NewRequest("GET", apiURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "Panoptic/1.0")

			resp, err := r.client.Do(req)
			if err != nil {
				log.Printf("[Radio] Fetch from %s failed: %v", base, err)
				break
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				log.Printf("[Radio] HTTP %d from %s", resp.StatusCode, base)
				break
			}

			var stations []RadioStation
			err = decodeJSON(resp.Body, &stations)
			resp.Body.Close()
			if err != nil {
				log.Printf("[Radio] JSON parse failed from %s: %v", base, err)
				break
			}

			all = append(all, stations...)
			if len(stations) < pageSize {
				break
			}
		}
		if len(all) > 0 {
			return all, nil
		}
	}

	return nil, fmt.Errorf("all radio-browser mirrors failed")
}

// streamAllStationsToFile downloads the full station list and writes it
// directly to the cache file, keeping memory usage low by never assembling the
// whole list in RAM.
func (r *RadioBrowserClient) streamAllStationsToFile(file string) (int, error) {
	const pageSize = 10000
	const maxPages = 30

	for _, base := range radioBrowserBases {
		tmp := file + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return 0, err
		}

		w := bufio.NewWriter(f)
		cleanup := func() {
			f.Close()
			os.Remove(tmp)
		}

		fetchedAtJSON, err := json.Marshal(time.Now())
		if err != nil {
			cleanup()
			return 0, err
		}
		if _, err := fmt.Fprintf(w, "{\n  \"fetched_at\": %s,\n  \"stations\": [", fetchedAtJSON); err != nil {
			cleanup()
			return 0, err
		}

		total := 0
		complete := false
		failed := false

		for page := 0; page < maxPages && !failed; page++ {
			apiURL := fmt.Sprintf("%s/stations?offset=%d&limit=%d", base, page*pageSize, pageSize)

			req, err := http.NewRequest("GET", apiURL, nil)
			if err != nil {
				failed = true
				break
			}
			req.Header.Set("User-Agent", "Panoptic/1.0")

			resp, err := r.client.Do(req)
			if err != nil {
				log.Printf("[Radio] Fetch from %s failed: %v", base, err)
				failed = true
				break
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				log.Printf("[Radio] HTTP %d from %s", resp.StatusCode, base)
				failed = true
				break
			}

			dec := json.NewDecoder(resp.Body)
			if _, err := dec.Token(); err != nil { // consume '['
				resp.Body.Close()
				failed = true
				break
			}

			pageCount := 0
			for dec.More() {
				var st RadioStation
				if err := dec.Decode(&st); err != nil {
					failed = true
					break
				}
				data, err := json.MarshalIndent(st, "", "    ")
				if err != nil {
					failed = true
					break
				}
				if total > 0 {
					w.WriteByte(',')
				}
				w.WriteString("\n    ")
				w.Write(data)
				total++
				pageCount++
			}
			resp.Body.Close()
			if failed {
				break
			}
			if pageCount < pageSize {
				complete = true
				break
			}
		}

		if failed || !complete {
			cleanup()
			continue
		}

		if _, err := w.WriteString("\n  ]\n}\n"); err != nil {
			cleanup()
			return 0, err
		}
		if err := w.Flush(); err != nil {
			cleanup()
			return 0, err
		}
		if err := f.Sync(); err != nil {
			cleanup()
			return 0, err
		}
		if err := f.Close(); err != nil {
			os.Remove(tmp)
			return 0, err
		}
		if err := os.Rename(tmp, file); err != nil {
			os.Remove(tmp)
			return 0, err
		}
		return total, nil
	}

	return 0, fmt.Errorf("all radio-browser mirrors failed")
}

// loadRadioCacheMeta reads only the metadata of the cached station list
// (fetch time and station count) without loading the stations into memory.
func loadRadioCacheMeta(file string) (time.Time, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return time.Time{}, 0, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var fetchedAt time.Time
	for {
		tok, err := dec.Token()
		if err != nil {
			return time.Time{}, 0, err
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}
		if key == "fetched_at" {
			if err := dec.Decode(&fetchedAt); err != nil {
				return time.Time{}, 0, err
			}
		} else if key == "stations" {
			if _, err := dec.Token(); err != nil { // consume '['
				return time.Time{}, 0, err
			}
			count := 0
			for dec.More() {
				var skip struct{}
				if err := dec.Decode(&skip); err != nil {
					return time.Time{}, 0, err
				}
				count++
			}
			return fetchedAt, count, nil
		}
	}
}

// searchStationsFile streams the cached station list from disk and returns the
// best matches without loading the whole file into memory. The second return
// value is false when the file cannot be read, letting callers fall back to the
// live radio-browser API.
func searchStationsFile(file, query string, limit int, byTag bool) ([]RadioStation, bool) {
	if limit <= 0 {
		limit = 5
	}

	f, err := os.Open(file)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}
		if key == "stations" {
			if _, err := dec.Token(); err != nil { // consume '['
				return nil, false
			}
			break
		}
	}

	q := strings.ToLower(strings.TrimSpace(query))
	matches := make([]RadioStation, 0)

	for dec.More() {
		var st RadioStation
		if err := dec.Decode(&st); err != nil {
			break
		}

		if byTag {
			if !strings.Contains(strings.ToLower(st.Tags), q) {
				continue
			}
		} else if !strings.Contains(strings.ToLower(st.Name), q) {
			continue
		}
		matches = append(matches, st)
	}

	sortRadioStationsByVotes(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}

	mode := "name"
	if byTag {
		mode = "tag"
	}
	log.Printf("[Radio] Stream search by %s '%s': %d match(es)", mode, query, len(matches))
	return matches, true
}

func (r *RadioBrowserClient) getCache() *RadioCache {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cache
}

func (r *RadioBrowserClient) setCache(cache *RadioCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = cache
}

func (c *RadioCache) isFresh(ttl time.Duration) bool {
	if ttl <= 0 {
		return true
	}
	return time.Since(c.FetchedAt) <= ttl
}

func (c *RadioCache) SearchByName(query string, limit int) []RadioStation {
	if limit <= 0 {
		limit = 5
	}

	q := strings.ToLower(strings.TrimSpace(query))
	matches := make([]RadioStation, 0)
	for _, st := range c.Stations {
		if strings.Contains(strings.ToLower(st.Name), q) {
			matches = append(matches, st)
		}
	}

	sortRadioStationsByVotes(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}

	log.Printf("[Radio] Cache search by name '%s': %d match(es)", query, len(matches))
	return matches
}

func (c *RadioCache) SearchByTag(query string, limit int) []RadioStation {
	if limit <= 0 {
		limit = 5
	}

	q := strings.ToLower(strings.TrimSpace(query))
	matches := make([]RadioStation, 0)
	for _, st := range c.Stations {
		if strings.Contains(strings.ToLower(st.Tags), q) {
			matches = append(matches, st)
		}
	}

	sortRadioStationsByVotes(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}

	log.Printf("[Radio] Cache search by tag '%s': %d match(es)", query, len(matches))
	return matches
}

func sortRadioStationsByVotes(stations []RadioStation) {
	sort.SliceStable(stations, func(i, j int) bool {
		return stations[i].Votes > stations[j].Votes
	})
}

func loadRadioCache(file string) (*RadioCache, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var cache RadioCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	if cache.FetchedAt.IsZero() {
		return nil, fmt.Errorf("cache file %s has no fetch timestamp", file)
	}

	return &cache, nil
}

func saveRadioCache(file string, cache *RadioCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0644)
}

func formatCacheAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}

type RadioSource struct {
	FfmpegPath string
	browser    *RadioBrowserClient
}

func NewRadioSource(ffmpegPath string) *RadioSource {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &RadioSource{
		FfmpegPath: ffmpegPath,
		browser:    NewRadioBrowserClient(),
	}
}

func (r *RadioSource) GetBrowser() *RadioBrowserClient {
	return r.browser
}

func (r *RadioSource) GetStream(streamURL string) gumbleffmpeg.Source {
	log.Printf("[Radio] Streaming: %s", streamURL)
	return gumbleffmpeg.SourceExec(r.FfmpegPath,
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-headers", "Icy-MetaData: 1\r\n",
		"-i", streamURL,
		"-f", "opus", "-")
}

func (r *RadioSource) GetStationLogo(logoURL string) string {
	if logoURL == "" {
		return ""
	}

	for _, candidate := range logoURLVariants(logoURL) {
		if encoded := r.downloadAndEncodeLogo(candidate); encoded != "" {
			return encoded
		}
	}
	return ""
}

// logoURLVariants returns the logo URL plus PNG fallbacks for formats Go cannot
// decode or that frequently 404, e.g. a missing favicon.ico backed by a PNG.
func logoURLVariants(logoURL string) []string {
	variants := []string{logoURL}

	base := logoURL
	if idx := strings.IndexAny(base, "?#"); idx != -1 {
		base = base[:idx]
	}
	lower := strings.ToLower(base)
	for _, ext := range []string{".ico", ".svg", ".webp", ".bmp"} {
		if strings.HasSuffix(lower, ext) {
			variants = append(variants, base[:len(base)-len(ext)]+".png")
		}
	}
	return variants
}

func (r *RadioSource) downloadAndEncodeLogo(logoURL string) string {
	log.Printf("[Radio] Downloading station logo: %s", logoURL)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(logoURL)
	if err != nil {
		log.Printf("[Radio] Logo download failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Radio] Logo HTTP %d", resp.StatusCode)
		return ""
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		log.Printf("[Radio] Logo is not an image: %s", contentType)
		return ""
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Radio] Logo read failed: %v", err)
		return ""
	}

	img, err := utils.DecodeImage(imgData)
	if err != nil {
		log.Printf("[Radio] Logo decode failed: %v", err)
		return ""
	}

	resized := utils.ResizeImage(img, 100, 100)

	encoded := utils.EncodeImageDataURI(resized, 4850)
	if encoded == "" {
		log.Printf("[Radio] Logo too large even at min quality")
	}
	return encoded
}

func decodeJSON(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

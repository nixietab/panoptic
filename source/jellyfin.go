package source

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nixietab/panoptic/jellyfin"
	"github.com/nixietab/panoptic/utils"
	"layeh.com/gumble/gumbleffmpeg"
)

type JellyfinSource struct {
	Client    *jellyfin.Client
	FfmpegPath string
	Name       string

	mu     sync.RWMutex
	cache  map[string]*jellyfin.Track
}

func NewJellyfinSource(client *jellyfin.Client, ffmpegPath, name string) *JellyfinSource {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &JellyfinSource{
		Client:     client,
		FfmpegPath: ffmpegPath,
		Name:       name,
		cache:      make(map[string]*jellyfin.Track),
	}
}

func (j *JellyfinSource) IsWhitelisted(url string) bool {
	return j.isJellyfinURL(url)
}

func (j *JellyfinSource) isJellyfinURL(url string) bool {
	return strings.Contains(url, j.Client.Address) && strings.Contains(url, "/Items/") && strings.Contains(url, "api_key=")
}

func (j *JellyfinSource) CacheTrack(url string, track *jellyfin.Track) {
	j.mu.Lock()
	j.cache[url] = track
	j.mu.Unlock()
}

func (j *JellyfinSource) getCachedTrack(url string) *jellyfin.Track {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.cache[url]
}

func (j *JellyfinSource) GetStream(url string) gumbleffmpeg.Source {
	log.Printf("[Jellyfin:%s] Streaming: %s", j.Name, url)
	return gumbleffmpeg.SourceExec(j.FfmpegPath, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-i", url, "-f", "opus", "-")
}

func (j *JellyfinSource) GetTitle(url string) string {
	if track := j.getCachedTrack(url); track != nil {
		return track.Title
	}
	return url
}

func (j *JellyfinSource) GetThumbnail(url string) string {
	track := j.getCachedTrack(url)
	if track == nil {
		itemID := j.extractItemID(url)
		if itemID == "" {
			return ""
		}
		thumbURL := fmt.Sprintf("%s/Items/%s/Images/Primary", j.Client.Address, itemID)
		return j.downloadAndEncode(thumbURL)
	}
	if track.Thumbnail == "" {
		return ""
	}
	return j.downloadAndEncode(track.Thumbnail)
}

func (j *JellyfinSource) extractItemID(url string) string {
	parts := strings.Split(url, "/Items/")
	if len(parts) < 2 {
		return ""
	}
	idPart := strings.Split(parts[1], "/")[0]
	idPart = strings.Split(idPart, "?")[0]
	return idPart
}

func (j *JellyfinSource) downloadAndEncode(thumbURL string) string {
	log.Printf("[Jellyfin:%s] Downloading thumbnail: %s", j.Name, thumbURL)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", thumbURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("X-Emby-Token", j.Client.GetToken())
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Jellyfin:%s] Thumbnail download failed: %v", j.Name, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Jellyfin:%s] Thumbnail HTTP %d", j.Name, resp.StatusCode)
		return ""
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	img, err := utils.DecodeImage(imgData)
	if err != nil {
		log.Printf("[Jellyfin:%s] Thumbnail decode failed: %v", j.Name, err)
		return ""
	}

	resized := utils.ResizeImage(img, 100, 100)

	encoded := utils.EncodeImageDataURI(resized, 4850)
	if encoded == "" {
		log.Printf("[Jellyfin:%s] Thumbnail too large even at min quality, skipping", j.Name)
	}
	return encoded
}

func (j *JellyfinSource) SearchAndCache(query string, limit int) ([]jellyfin.Track, error) {
	tracks, err := j.Client.Search(query, limit)
	if err != nil {
		return nil, err
	}

	for i := range tracks {
		j.CacheTrack(tracks[i].URL, &tracks[i])
	}

	return tracks, nil
}

func (j *JellyfinSource) GetAlbumArt(trackURL string) string {
	track := j.getCachedTrack(trackURL)
	if track == nil {
		return ""
	}
	return j.GetThumbnail(trackURL)
}

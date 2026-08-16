package radio

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Metadata struct {
	Title    string
	ArtURL   string
	Station  string
}

type MetadataManager struct {
	mu       sync.RWMutex
	current  Metadata
	stopCh   chan struct{}
	running  bool
}

func NewMetadataManager() *MetadataManager {
	return &MetadataManager{}
}

func (mm *MetadataManager) GetCurrent() Metadata {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.current
}

func (mm *MetadataManager) Stop() {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if mm.running && mm.stopCh != nil {
		close(mm.stopCh)
		mm.stopCh = nil
		mm.running = false
	}
}

func (mm *MetadataManager) StartPolling(streamURL string, interval time.Duration, onUpdate func(Metadata)) {
	mm.Stop()

	mm.mu.Lock()
	mm.stopCh = make(chan struct{})
	mm.running = true
	mm.mu.Unlock()

	go mm.pollLoop(streamURL, interval, onUpdate)
}

func (mm *MetadataManager) pollLoop(streamURL string, interval time.Duration, onUpdate func(Metadata)) {
	var lastTitle string

	for {
		mm.mu.RLock()
		stopCh := mm.stopCh
		mm.mu.RUnlock()

		if stopCh == nil {
			return
		}

		meta := mm.fetchMetadata(streamURL)
		if meta != nil {
			mm.mu.Lock()
			mm.current = *meta
			mm.mu.Unlock()

			if meta.Title != "" && meta.Title != lastTitle {
				lastTitle = meta.Title
				log.Printf("[Radio:Metadata] Now playing: %s", meta.Title)
				if onUpdate != nil {
					onUpdate(*meta)
				}
			}
		}

		select {
		case <-stopCh:
			return
		case <-time.After(interval):
		}
	}
}

func (mm *MetadataManager) fetchMetadata(streamURL string) *Metadata {
	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "Panoptic/1.0")

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Radio:Metadata] HTTP request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	metaIntStr := resp.Header.Get("icy-metaint")
	if metaIntStr == "" {
		log.Printf("[Radio:Metadata] No icy-metaint header, trying to read stream title from headers")
		return mm.parseHeaders(resp)
	}

	metaInt, err := strconv.Atoi(metaIntStr)
	if err != nil {
		log.Printf("[Radio:Metadata] Invalid icy-metaint: %s", metaIntStr)
		return nil
	}

	return mm.readICYMetadata(resp.Body, metaInt)
}

func (mm *MetadataManager) parseHeaders(resp *http.Response) *Metadata {
	meta := &Metadata{}

	if name := resp.Header.Get("icy-name"); name != "" {
		meta.Station = name
	}
	if title := resp.Header.Get("icy-title"); title != "" {
		meta.Title = title
	}
	if art := resp.Header.Get("icy-url"); art != "" {
		meta.ArtURL = art
	}

	if meta.Title == "" && meta.Station == "" {
		return nil
	}
	return meta
}

func (mm *MetadataManager) readICYMetadata(body io.ReadCloser, metaInt int) *Metadata {
	deadline := time.Now().Add(8 * time.Second)

	for time.Now().Before(deadline) {
		limited := io.LimitReader(body, int64(metaInt))
		if _, err := io.Copy(io.Discard, limited); err != nil {
			log.Printf("[Radio:Metadata] Failed to skip audio data: %v", err)
			return nil
		}

		lengthByte := make([]byte, 1)
		_, err := io.ReadFull(body, lengthByte)
		if err != nil {
			log.Printf("[Radio:Metadata] Failed to read metadata length: %v", err)
			return nil
		}

		metaLen := int(lengthByte[0]) * 16
		if metaLen == 0 {
			continue
		}

		metaBytes := make([]byte, metaLen)
		_, err = io.ReadFull(body, metaBytes)
		if err != nil {
			log.Printf("[Radio:Metadata] Failed to read metadata: %v", err)
			return nil
		}

		if meta := parseICYMetadataBlock(string(metaBytes)); meta != nil {
			return meta
		}
	}

	return nil
}

func parseICYMetadataBlock(raw string) *Metadata {
	raw = strings.TrimRight(raw, "\x00")
	if raw == "" {
		return nil
	}

	meta := &Metadata{}

	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		eqIdx := strings.Index(part, "=")
		if eqIdx == -1 {
			continue
		}

		key := strings.TrimSpace(part[:eqIdx])
		value := strings.TrimSpace(part[eqIdx+1:])
		value = strings.Trim(value, "'\"")

		switch key {
		case "StreamTitle":
			if value != "" {
				meta.Title = value
			}
		case "StreamUrl":
			if value != "" {
				meta.ArtURL = value
			}
		}
	}

	if meta.Title == "" && meta.ArtURL == "" {
		return nil
	}

	return meta
}

func FetchMetadataOnce(streamURL string) *Metadata {
	mm := &MetadataManager{}
	return mm.fetchMetadata(streamURL)
}

func ParseStreamTitle(title string) (artist, song string) {
	if title == "" {
		return "", ""
	}

	parts := strings.SplitN(title, " - ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", strings.TrimSpace(title)
}

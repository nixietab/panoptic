package source

import (
	"log"
	"strings"

	"layeh.com/gumble/gumbleffmpeg"
)

type HTTPSource struct {
	FfmpegPath string
}

func NewHTTPSource(ffmpegPath string) *HTTPSource {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &HTTPSource{FfmpegPath: ffmpegPath}
}

func (h *HTTPSource) IsWhitelisted(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func (h *HTTPSource) GetStream(url string) gumbleffmpeg.Source {
	log.Printf("[HTTP] Streaming via ffmpeg: %s", url)
	return gumbleffmpeg.SourceExec(h.FfmpegPath, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-i", url, "-f", "opus", "-")
}

func (h *HTTPSource) GetTitle(url string) string {
	return url
}

func (h *HTTPSource) GetThumbnail(url string) string {
	return ""
}

package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/nixietab/panoptic/utils"
	"github.com/nixietab/ytsr"
	"layeh.com/gumble/gumbleffmpeg"
)

var youtubeDomains = []string{
	"youtube.com",
	"www.youtube.com",
	"youtu.be",
	"m.youtube.com",
}

type YouTubeSource struct {
	YtdlpPath    string
	SearchClient *ytsr.Client
}

func NewYouTubeSource(ytdlpPath string) *YouTubeSource {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	return &YouTubeSource{
		YtdlpPath:    ytdlpPath,
		SearchClient: ytsr.New(),
	}
}

func (yt *YouTubeSource) IsWhitelisted(u string) bool {
	for _, domain := range youtubeDomains {
		if strings.Contains(u, domain) {
			return true
		}
	}
	return false
}

func (yt *YouTubeSource) GetStream(u string) gumbleffmpeg.Source {
	return gumbleffmpeg.SourceExec(yt.YtdlpPath, "--no-playlist", "-f", "bestaudio", "-o", "-", u)
}

func (yt *YouTubeSource) GetTitle(u string) string {
	cmd := exec.Command(yt.YtdlpPath, "--no-playlist", "-e", u)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		log.Printf("[Title] yt-dlp failed for %s: %v", u, err)
		return ""
	}
	title := strings.TrimSpace(output.String())
	if title != "" {
		return title
	}
	return u
}

func (yt *YouTubeSource) GetThumbnail(u string) string {
	videoID, err := ytsr.ExtractVideoID(u)
	if err != nil {
		log.Println("[Thumbnail] No video ID found in:", u)
		return ""
	}

	thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
	log.Println("[Thumbnail] Downloading:", thumbURL)

	resp, err := http.Get(thumbURL)
	if err != nil {
		log.Println("[Thumbnail] Download failed:", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("[Thumbnail] HTTP", resp.StatusCode, "for", thumbURL)
		return ""
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("[Thumbnail] Read failed:", err)
		return ""
	}

	img, err := utils.DecodeImage(imgData)
	if err != nil {
		log.Println("[Thumbnail] Decode failed:", err)
		return ""
	}

	resized := utils.ResizeImage(img, 100, 100)

	encoded := utils.EncodeImageDataURI(resized, 4850)
	if encoded == "" {
		log.Println("[Thumbnail] Too large even at min quality for", videoID)
	}
	return encoded
}

func (yt *YouTubeSource) Search(query string) string {
	video, err := yt.SearchClient.First(context.Background(), query)
	if err != nil {
		return ""
	}
	return video.URL
}

func (yt *YouTubeSource) SearchWithInfo(query string) *ytsr.Video {
	video, err := yt.SearchClient.First(context.Background(), query)
	if err != nil {
		return nil
	}
	return &video
}

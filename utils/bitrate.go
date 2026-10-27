package utils

import (
	"bytes"
	"encoding/json"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var bitrateCache sync.Map

type FFProbeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		BitRate   string `json:"bit_rate"`
	} `json:"streams"`
	Format struct {
		BitRate string `json:"bit_rate"`
	} `json:"format"`
}

func GetBitrate(path string) string {
	if cached, ok := bitrateCache.Load(path); ok {
		return cached.(string)
	}

	var bitrate string
	if strings.HasPrefix(path, "http") {
		bitrate = getRemoteBitrate(path)
	} else {
		bitrate = probeBitrate(path)
	}

	if bitrate != "" {
		bitrateCache.Store(path, bitrate)
	}
	return bitrate
}

func getRemoteBitrate(path string) string {
	if strings.Contains(path, "/Items/") {
		return probeBitrateJellyfin(path)
	}

	cmd := exec.Command("yt-dlp", "--no-playlist", "-f", "bestaudio", "-g", path)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return ""
	}

	resolved := strings.TrimSpace(output.String())
	if resolved == "" {
		return ""
	}
	return probeBitrate(resolved)
}

func probeBitrateJellyfin(path string) string {
	if path == "" {
		return ""
	}

	if idx := strings.Index(path, "api_key="); idx >= 0 {
		token := path[idx+8:]
		if amp := strings.Index(token, "&"); amp >= 0 {
			token = token[:amp]
		}
		if token != "" {
			headers := "Authorization: MediaBrowser Token=\"" + token + "\", Client=\"Panoptic\", Device=\"Go\", DeviceId=\"panoptic-bot\", Version=\"1.0.0\"\r\nX-Emby-Token: " + token
			cmd := exec.Command("ffprobe",
				"-v", "quiet",
				"-print_format", "json",
				"-show_streams",
				"-show_format",
				"-headers", headers,
				path)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &bytes.Buffer{}
			if err := cmd.Run(); err == nil {
				var probeData FFProbeOutput
				if json.Unmarshal(output.Bytes(), &probeData) == nil {
					for _, stream := range probeData.Streams {
						if stream.CodecType == "audio" && stream.BitRate != "" {
							return formatBitrate(stream.BitRate)
						}
					}
					if probeData.Format.BitRate != "" {
						return formatBitrate(probeData.Format.BitRate)
					}
				}
			}
		}
	}

	return probeBitrate(path)
}

func probeBitrate(path string) string {
	if path == "" {
		return ""
	}

	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		path)

	var output bytes.Buffer
	cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		log.Printf("[Bitrate] ffprobe failed: %v", err)
		return ""
	}

	var probeData FFProbeOutput
	err = json.Unmarshal(output.Bytes(), &probeData)
	if err != nil {
		return ""
	}

	for _, stream := range probeData.Streams {
		if stream.CodecType == "audio" && stream.BitRate != "" {
			return formatBitrate(stream.BitRate)
		}
	}

	if probeData.Format.BitRate != "" {
		return formatBitrate(probeData.Format.BitRate)
	}

	return ""
}

func formatBitrate(bitrateStr string) string {
	bitrate, err := strconv.ParseFloat(bitrateStr, 64)
	if err != nil {
		return ""
	}
	kbps := int(bitrate / 1000)
	return strconv.Itoa(kbps) + " kbps"
}

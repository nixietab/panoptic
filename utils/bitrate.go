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

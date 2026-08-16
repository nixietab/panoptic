package config

import (
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig
	Bot       BotConfig
	Audio     AudioConfig
	Whitelist WhitelistConfig
	Jellyfin  JellyfinConfig
	Radio     RadioConfig
}

type ServerConfig struct {
	Host       string `toml:"host"`
	Port       int    `toml:"port"`
	Password   string `toml:"password"`
	Insecure   bool   `toml:"insecure"`
	CertFile   string `toml:"cert_file"`
	KeyFile    string `toml:"key_file"`
}

type BotConfig struct {
	Username     string `toml:"username"`
	Channel      string `toml:"channel"`
	Prefix       string `toml:"prefix"`
	ShowDuration bool   `toml:"show_duration"`
}

type AudioConfig struct {
	Volume       float32 `toml:"volume"`
	YtdlpPath    string  `toml:"ytdlp_path"`
	FfmpegPath   string  `toml:"ffmpeg_path"`
}

type WhitelistConfig struct {
	Enabled bool     `toml:"enabled"`
	Domains []string `toml:"domains"`
}

type JellyfinConfig struct {
	Enabled bool               `toml:"enabled"`
	Servers []JellyfinServer   `toml:"servers"`
}

type JellyfinServer struct {
	Name     string `toml:"name"`
	Address  string `toml:"address"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Insecure bool   `toml:"insecure"`
}

type RadioConfig struct {
	Enabled       bool   `toml:"enabled"`
	PollInterval  int    `toml:"poll_interval"`
	CacheEnabled  bool   `toml:"cache_enabled"`
	CacheTTLHours int    `toml:"cache_ttl_hours"`
	CacheFile     string `toml:"cache_file"`
	CacheInMemory bool   `toml:"cache_in_memory"`
	QuietUpdates  bool   `toml:"quiet_updates"`
}

func (w *WhitelistConfig) IsAllowed(url string) bool {
	if !w.Enabled {
		return true
	}
	if len(w.Domains) == 0 {
		return true
	}
	for _, domain := range w.Domains {
		if strings.Contains(url, domain) {
			return true
		}
	}
	return false
}

func Load(path string) *Config {
	defaultConfig := &Config{
		Server: ServerConfig{
			Host:     "localhost",
			Port:     64738,
			Password: "",
			Insecure: false,
		},
		Bot: BotConfig{
			Username:     "panoptic",
			Channel:      "",
			Prefix:       "!",
			ShowDuration: true,
		},
		Audio: AudioConfig{
			Volume:     0.3,
			YtdlpPath:  "yt-dlp",
			FfmpegPath: "ffmpeg",
		},
		Whitelist: WhitelistConfig{
			Enabled: false,
			Domains: []string{},
		},
		Jellyfin: JellyfinConfig{
			Enabled: false,
			Servers: []JellyfinServer{},
		},
		Radio: RadioConfig{
			Enabled:       true,
			PollInterval:  30,
			CacheEnabled:  true,
			CacheTTLHours: 24,
			CacheFile:     "radio_stations.json",
			CacheInMemory: true,
			QuietUpdates:  false,
		},
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Println("Config file not found, using defaults")
		return defaultConfig
	}

	if _, err := toml.DecodeFile(path, defaultConfig); err != nil {
		log.Println("Failed to load config, using defaults:", err)
		return defaultConfig
	}

	log.Println("Configuration loaded from", path)
	if defaultConfig.Whitelist.Enabled {
		log.Printf("Whitelist enabled: %v", defaultConfig.Whitelist.Domains)
	} else {
		log.Println("Whitelist disabled - all URLs allowed")
	}
	if defaultConfig.Jellyfin.Enabled {
		log.Printf("Jellyfin enabled: %d server(s) configured", len(defaultConfig.Jellyfin.Servers))
	} else {
		log.Println("Jellyfin disabled")
	}
	if defaultConfig.Radio.Enabled {
		if defaultConfig.Radio.PollInterval > 0 {
			log.Printf("Radio enabled: poll interval %ds", defaultConfig.Radio.PollInterval)
		} else {
			log.Println("Radio enabled: metadata polling disabled")
		}
		if defaultConfig.Radio.CacheEnabled {
			log.Printf("Radio station cache enabled: file=%s ttl=%dh", defaultConfig.Radio.CacheFile, defaultConfig.Radio.CacheTTLHours)
			if defaultConfig.Radio.CacheInMemory {
				log.Println("Radio station cache: loaded into RAM")
			} else {
				log.Println("Radio station cache: streamed from disk (low memory mode)")
			}
		} else {
			log.Println("Radio station cache disabled")
		}
		if defaultConfig.Radio.QuietUpdates {
			log.Println("Radio updates: bot description only (quiet_updates)")
		} else {
			log.Println("Radio updates: posted to channel and bot description")
		}
	} else {
		log.Println("Radio disabled")
	}
	return defaultConfig
}

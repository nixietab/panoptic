package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nixietab/panoptic/commands"
	"github.com/nixietab/panoptic/config"
	"github.com/nixietab/panoptic/jellyfin"
	"github.com/nixietab/panoptic/player"
	"github.com/nixietab/panoptic/source"
	"github.com/nixietab/panoptic/utils"

	"layeh.com/gumble/gumble"
	"layeh.com/gumble/gumbleutil"
)

func main() {
	configPath := flag.String("config", "config.toml", "Path to config file")
	flag.Parse()

	cfg := config.Load(*configPath)

	ytSource := source.NewYouTubeSource(cfg.Audio.YtdlpPath)
	httpSource := source.NewHTTPSource(cfg.Audio.FfmpegPath)

	registry := source.NewRegistry(&cfg.Whitelist)
	registry.Register(ytSource)

	commands.SetYouTubeSource(ytSource)

	if cfg.Jellyfin.Enabled && len(cfg.Jellyfin.Servers) > 0 {
		var jellyfinSources []*source.JellyfinSource
		for _, srv := range cfg.Jellyfin.Servers {
			client := jellyfin.NewClient(srv.Address, srv.User, srv.Password, srv.Insecure)
			if err := client.Authenticate(); err != nil {
				log.Printf("[Jellyfin] Failed to authenticate to %s: %v", srv.Address, err)
				continue
			}
			jfSrc := source.NewJellyfinSource(client, cfg.Audio.FfmpegPath, srv.Name)
			registry.Register(jfSrc)
			jellyfinSources = append(jellyfinSources, jfSrc)
			log.Printf("[Jellyfin] Connected to %s (%s)", srv.Name, srv.Address)
		}
		commands.SetJellyfinSources(jellyfinSources)
		commands.RegisterJellyfinCommands(len(jellyfinSources))
	}

	registry.Register(httpSource)

	if cfg.Radio.Enabled {
		radioSrc := source.NewRadioSource(cfg.Audio.FfmpegPath)
		commands.SetRadioSource(radioSrc, cfg.Radio.PollInterval)
		commands.RegisterRadioCommands()

		if cfg.Radio.CacheEnabled {
			go func() {
				if err := radioSrc.GetBrowser().LoadCache(cfg.Radio.CacheFile, cfg.Radio.CacheTTLHours, cfg.Radio.CacheInMemory); err != nil {
					log.Printf("[Radio] Station cache unavailable, using live API: %v", err)
				}
			}()
		}
		log.Println("Radio enabled")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	for {
		log.Println("Connecting to", cfg.Server.Host, "...")

		disconnected := make(chan struct{})
		var p *player.Player
		var client *gumble.Client

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered from panic: %v", r)
				}
				select {
				case <-disconnected:
				default:
					close(disconnected)
				}
			}()

			gConfig := gumble.NewConfig()
			gConfig.Username = cfg.Bot.Username
			gConfig.Password = cfg.Server.Password

			gConfig.Attach(gumbleutil.AutoBitrate)
			gConfig.Attach(gumbleutil.Listener{
				Connect: func(e *gumble.ConnectEvent) {
					client = e.Client

					if cfg.Bot.Channel != "" {
						if ch := e.Client.Channels.Find(cfg.Bot.Channel); ch != nil {
							e.Client.Self.Move(ch)
							log.Printf("[Connect] Moving to channel: %s", cfg.Bot.Channel)
						}
					}

					p = player.New(e.Client, cfg, registry)
					p.Queue.Load()

					log.Printf("[Connect] Panoptic ready! Username: %s | Channel: %s", cfg.Bot.Username, e.Client.Self.Channel.Name)
				},

				TextMessage: func(e *gumble.TextMessageEvent) {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("Recovered from panic in TextMessage: %v", r)
						}
					}()

					if e.Sender == nil || p == nil {
						return
					}

					isPrivate := len(e.TextMessage.Channels) == 0

				if isPrivate {
					log.Printf("PM from %s: %s", e.Sender.Name, utils.LogSafeMessage(e.Message))
				} else {
					log.Printf("[%s] %s: %s", e.Sender.Channel.Name, e.Sender.Name, utils.LogSafeMessage(e.Message))
				}

					if commands.IsCommand(e.Message, isPrivate, cfg.Bot.Username, cfg) {
						go commands.Dispatch(p, e.Message, isPrivate, e.Sender.Name)
					}
				},

				Disconnect: func(e *gumble.DisconnectEvent) {
					log.Println("Disconnected:", e.Type)

					if p != nil {
						p.Queue.Save()
					}

					select {
					case <-disconnected:
					default:
						close(disconnected)
					}
				},
			})

			address := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))

			var tlsConfig tls.Config
			if cfg.Server.Insecure {
				tlsConfig.InsecureSkipVerify = true
			}
			if cfg.Server.CertFile != "" {
				keyFile := cfg.Server.KeyFile
				if keyFile == "" {
					keyFile = cfg.Server.CertFile
				}
				cert, err := tls.LoadX509KeyPair(cfg.Server.CertFile, keyFile)
				if err != nil {
					log.Fatalf("Failed to load certificate: %s", err)
				}
				tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
			}

			dialer := &net.Dialer{Timeout: 10 * time.Second}
			_, err := gumble.DialWithDialer(dialer, address, gConfig, &tlsConfig)
			if err != nil {
				log.Printf("Dial error: %s", err)
				return
			}

			<-disconnected
		}()

		select {
		case <-stop:
			log.Println("Shutting down...")
			if client != nil {
				client.Disconnect()
			}
			return

		case <-disconnected:
			log.Println("Reconnecting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}
}

func init() {
	fmt.Println(`
vamo arrancando manga de giles
	`)
}

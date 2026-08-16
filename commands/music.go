package commands

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nixietab/panoptic/player"
	"github.com/nixietab/panoptic/radio"
	"github.com/nixietab/panoptic/source"
	"github.com/nixietab/panoptic/utils"
)

var ytSource *source.YouTubeSource
var jellyfinSources []*source.JellyfinSource
var radioSource *source.RadioSource
var radioPollInterval time.Duration

func SetYouTubeSource(src *source.YouTubeSource) {
	ytSource = src
}

func SetJellyfinSources(srcs []*source.JellyfinSource) {
	jellyfinSources = srcs
}

func SetRadioSource(src *source.RadioSource, pollInterval int) {
	radioSource = src
	if pollInterval > 0 {
		radioPollInterval = time.Duration(pollInterval) * time.Second
	} else {
		radioPollInterval = 0
	}
}

func cmdPlay(p *player.Player, sender string, isPrivate bool, arg string) {
	if arg == "" {
		if p.Resume() {
			log.Printf("[Play] Resumed playback")
			return
		}
		log.Printf("[Play] Resuming playback")
		p.PlayCurrent()
		return
	}

	var path, title string
	var duration time.Duration

	if strings.HasPrefix(arg, "http") || strings.HasPrefix(arg, "www.") {
		cleanURL := strings.TrimSpace(arg)
		if !p.Source.IsWhitelisted(cleanURL) {
			p.SendReply("URL not supported", sender, isPrivate)
			return
		}
		path = cleanURL
		title = p.Source.GetTitle(cleanURL)
		if title == "" {
			title = cleanURL
		}
		log.Printf("[Play] URL: %s -> %s", cleanURL, title)
	} else {
		if ytSource == nil {
			p.SendReply("YouTube search not available", sender, isPrivate)
			return
		}
		log.Printf("[Play] Searching YouTube: %s", arg)
		video := ytSource.SearchWithInfo(arg)
		if video == nil {
			p.SendReply("Search failed: "+utils.EscapeHTML(arg), sender, isPrivate)
			return
		}
		path = video.URL
		title = video.Title
		duration = video.Duration
		log.Printf("[Play] Found: %s (%s)", title, video.ID)
	}

	p.Queue.Add(path, title, duration)
	p.SendReply(formatQueued(title, duration, p.Config.Bot.ShowDuration), sender, isPrivate)

	if !p.IsPlaying() {
		p.PlayLast()
	}
}

func cmdPause(p *player.Player, sender string, isPrivate bool, arg string) {
	if err := p.Pause(); err != nil {
		p.SendReply("Pause failed: "+utils.EscapeHTML(err.Error()), sender, isPrivate)
		return
	}
	p.SendReply("Paused. Use .play to resume.", sender, isPrivate)
}

func cmdSkip(p *player.Player, sender string, isPrivate bool, arg string) {
	amount := 1
	if arg != "" {
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			amount = n
		}
	}
	log.Printf("[Skip] Skipping %d track(s)", amount)
	p.Skip(amount)
}

func cmdStop(p *player.Player, sender string, isPrivate bool, arg string) {
	log.Printf("[Stop] Stopping playback")
	p.Stop(true)
	p.SendReply("Playback stopped.", sender, isPrivate)
}

func cmdSeek(p *player.Player, sender string, isPrivate bool, arg string) {
	if p.IsRadioMode() {
		return
	}

	offset, ok := parseSeekArg(arg)
	if !ok {
		p.SendReply("Usage: .sk <seconds> or .sk <mm:ss>", sender, isPrivate)
		return
	}

	if p.Seek(offset) {
		log.Printf("[Seek] Jumped to %s", player.FormatDuration(offset))
		p.SendReply("Jumped to "+player.FormatDuration(offset), sender, isPrivate)
	} else {
		p.SendReply("Nothing is playing.", sender, isPrivate)
	}
}

func parseSeekArg(arg string) (time.Duration, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, false
	}

	if strings.Contains(arg, ":") {
		var total time.Duration
		for _, part := range strings.Split(arg, ":") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 0 {
				return 0, false
			}
			total = total*60 + time.Duration(n)*time.Second
		}
		return total, true
	}

	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

func cmdList(p *player.Player, sender string, isPrivate bool, arg string) {
	tracks := p.Queue.List()
	if len(tracks) == 0 {
		p.SendReply("Queue is empty.", sender, isPrivate)
		return
	}

	var sb strings.Builder
	sb.WriteString("<table border=\"1\" cellpadding=\"5\"><tr><td><h3>Queue</h3><ul>")
	for i, title := range tracks {
		sb.WriteString("<li>")
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, utils.EscapeHTML(title)))
		sb.WriteString("</li>")
	}
	sb.WriteString("</ul>")
	sb.WriteString(fmt.Sprintf("<p><b>%d</b> tracks queued</p>", p.Queue.Count()))
	sb.WriteString("</td></tr></table>")

	p.SendReply(sb.String(), sender, isPrivate)
}

func cmdVol(p *player.Player, sender string, isPrivate bool, arg string) {
	if arg != "" {
		if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= 100 {
			p.SetVolume(0.01 * float32(n))
			p.SendReply(fmt.Sprintf("Volume set to %d%%", n), sender, isPrivate)
			return
		}
		p.SendReply("Invalid volume. Use 1-100.", sender, isPrivate)
		return
	}

	current := int(math.Ceil(float64(p.Volume * 100)))
	p.SendReply(fmt.Sprintf("Current volume: %d%%", current), sender, isPrivate)
}

func cmdHelp(p *player.Player, sender string, isPrivate bool, arg string) {
	help := `<table cellpadding="5">
<tr><td colspan="3"><h3>Panoptic Commands</h3></td></tr>
<tr><td><b>Command</b></td><td><b>Alias</b></td><td><b>Description</b></td></tr>
<tr><td>.play [url/search]</td><td>.p</td><td>Play a song or search YouTube; no args resumes paused playback</td></tr>
<tr><td>.pause</td><td>.pa</td><td>Pause playback</td></tr>
<tr><td>.skip [n]</td><td>.s</td><td>Skip current or n tracks</td></tr>
<tr><td>.seek [seconds | mm:ss]</td><td>.sk</td><td>Jump to a position in the current track</td></tr>
<tr><td>.stop</td><td></td><td>Stop playback</td></tr>
<tr><td>.list</td><td>.queue</td><td>Show the queue</td></tr>
<tr><td>.vol [1-100]</td><td></td><td>Set or show volume</td></tr>
<tr><td>.summon</td><td></td><td>Summon bot to your channel</td></tr>
<tr><td>.radio [url/search]</td><td>.r</td><td>Play online radio or a stream URL</td></tr>
<tr><td>.radio stop</td><td>.r stop</td><td>Stop radio playback</td></tr>
<tr><td>.j [query]</td><td>.jellyfin</td><td>Search first Jellyfin server</td></tr>
<tr><td>.j2, .j3...</td><td></td><td>Search additional Jellyfin servers (command number increments per server)</td></tr>
<tr><td>.about</td><td></td><td>Show information about the bot</td></tr>
<tr><td>.help</td><td></td><td>Show this help</td></tr>
</table>`
	p.SendReply(help, sender, isPrivate)
}

func cmdAbout(p *player.Player, sender string, isPrivate bool, arg string) {
	about := `<table cellpadding="2" cellspacing="0" style="border-collapse:collapse"><tr><td>
<h3>Panoptic 1.0.0</h3>
<p style="margin:0">Simple and fast music bot for Mumble.</p>
<p style="margin:0"><a href="https://github.com/nixietab/panoptic">github.com/nixietab/panoptic</a></p>
<p style="margin:0">Licensed under AGPL 3.0.</p>
<br>` + buttonLogo() + `
</td></tr></table>`
	p.SendReply(about, sender, isPrivate)
}

// buttonLogo returns the 88x31 badge as a base64 data URI. It reads the local
// 88x31.png and re-encodes it to fit within Mumble's message budget so clients
// render it inline without external hosting.
func buttonLogo() string {
	data, err := os.ReadFile("88x31.png")
	if err != nil {
		log.Printf("[About] Failed to read 88x31.png: %v", err)
		return ""
	}
	img, err := utils.DecodeImage(data)
	if err != nil {
		log.Printf("[About] Failed to decode 88x31.png: %v", err)
		return ""
	}
	return utils.EncodeImageDataURI(img, 4850)
}

func cmdSummon(p *player.Player, sender string, isPrivate bool, arg string) {
	if p.SummonToUser(sender) {
		p.SendReply("Summoned to your channel.", sender, isPrivate)
	} else {
		p.SendReply("Could not find you.", sender, isPrivate)
	}
}

func cmdRadio(p *player.Player, sender string, isPrivate bool, arg string) {
	if radioSource == nil {
		p.SendReply("Radio not available", sender, isPrivate)
		return
	}

	arg = strings.TrimSpace(arg)

	if arg == "" {
		if p.IsRadioMode() {
			meta := radio.FetchMetadataOnce("")
			if meta != nil && meta.Title != "" {
				p.SendReply("Radio playing: "+utils.EscapeHTML(meta.Title), sender, isPrivate)
			} else {
				p.SendReply("Radio is playing (no metadata available)", sender, isPrivate)
			}
		} else {
			p.SendReply("Usage: .r <url> or .r <search query>", sender, isPrivate)
		}
		return
	}

	if strings.EqualFold(arg, "stop") {
		if p.IsRadioMode() {
			p.StopRadio()
			p.SendReply("Radio stopped.", sender, isPrivate)
		} else {
			p.SendReply("Radio is not playing.", sender, isPrivate)
		}
		return
	}

	var streamURL, stationName, logoImg string

	if strings.HasPrefix(arg, "http") || strings.HasPrefix(arg, "www.") {
		streamURL = strings.TrimSpace(arg)
		stationName = streamURL

		if radioSource.GetBrowser() != nil {
			stations := radioSource.GetBrowser().SearchByName(streamURL, 1)
			if len(stations) > 0 {
				stationName = stations[0].Name
				if stations[0].LogoURL != "" {
					logoImg = radioSource.GetStationLogo(stations[0].LogoURL)
				}
				streamURL = stations[0].URLResolved
			}
		}
	} else {
		log.Printf("[Radio] Searching: %s", arg)
		stations := radioSource.GetBrowser().SearchByName(arg, 5)
		if len(stations) == 0 {
			stations = radioSource.GetBrowser().SearchByTag(arg, 5)
		}

		if len(stations) == 0 {
			p.SendReply("No radio stations found for: "+utils.EscapeHTML(arg), sender, isPrivate)
			return
		}

		best := stations[0]
		streamURL = best.URLResolved
		stationName = best.Name
		if best.LogoURL != "" {
			logoImg = radioSource.GetStationLogo(best.LogoURL)
		}
	}

	log.Printf("[Radio] Playing: %s (%s)", stationName, streamURL)

	p.PlayRadio(streamURL, stationName, logoImg, radioPollInterval, func(meta radio.Metadata) {
		if meta.Title != "" && p.IsRadioMode() {
			artist, song := radio.ParseStreamTitle(meta.Title)
			display := meta.Title
			if artist != "" && song != "" {
				display = artist + " - " + song
			}

			np := "<table cellpadding=\"2\" cellspacing=\"0\"><tr>"
			np += "<td>&nbsp;<b><u><small>Now Playing (" + utils.EscapeHTML(stationName) + ")</small></u></b><br>"
			np += "<font size=\"+1\">" + utils.EscapeHTML(display) + "</font>"
			np += "</td></tr></table>"

			if !p.Config.Radio.QuietUpdates {
				p.SendReply(np, "", false)
			}
			p.SetComment(np)
		}
	})
}

func makeJellyfinCmd(serverIndex int) CommandFunc {
	return func(p *player.Player, sender string, isPrivate bool, arg string) {
		if arg == "" {
			p.SendReply("Usage: .j <search query>", sender, isPrivate)
			return
		}

		if serverIndex >= len(jellyfinSources) {
			p.SendReply("Jellyfin server not available", sender, isPrivate)
			return
		}

		src := jellyfinSources[serverIndex]
		log.Printf("[Jellyfin:%s] Searching: %s", src.Name, arg)

		tracks, err := src.SearchAndCache(arg, 1)
		if err != nil {
			log.Printf("[Jellyfin:%s] Search failed: %v", src.Name, err)
			p.SendReply("Search failed: "+utils.EscapeHTML(err.Error()), sender, isPrivate)
			return
		}

		if len(tracks) == 0 {
			p.SendReply("No results for: "+utils.EscapeHTML(arg), sender, isPrivate)
			return
		}

		track := tracks[0]
		p.Queue.Add(track.URL, track.Title, track.Duration)
		p.SendReply(formatQueued(track.Title, track.Duration, p.Config.Bot.ShowDuration), sender, isPrivate)

		if !p.IsPlaying() {
			p.PlayLast()
		}
	}
}

// formatQueued builds the "Queued: <title> (<duration>)" reply, omitting the
// duration when the feature is disabled in config or the length is unknown.
func formatQueued(title string, duration time.Duration, show bool) string {
	if !show || duration <= 0 {
		return "Queued: " + utils.EscapeHTML(title)
	}
	return fmt.Sprintf("Queued: %s (%s)", utils.EscapeHTML(title), player.FormatDuration(duration))
}

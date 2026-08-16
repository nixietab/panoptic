package player

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nixietab/panoptic/config"
	"github.com/nixietab/panoptic/queue"
	"github.com/nixietab/panoptic/radio"
	"github.com/nixietab/panoptic/source"
	"github.com/nixietab/panoptic/utils"

	"layeh.com/gumble/gumble"
	"layeh.com/gumble/gumbleffmpeg"
	_ "layeh.com/gumble/opus"
)

type Player struct {
	stream      *gumbleffmpeg.Stream
	Client      *gumble.Client
	Queue       *queue.Queue
	Volume      float32
	Config      *config.Config
	Source      *source.Registry
	mu          sync.Mutex
	wantsToStop bool

	paused            bool
	pausedAt          time.Duration
	streamStartOffset time.Duration

	radioMode     bool
	radioMetadata *radio.MetadataManager
	radioTitle    string
	radioArtImg   string
}

func New(client *gumble.Client, cfg *config.Config, src *source.Registry) *Player {
	return &Player{
		stream: nil,
		Client: client,
		Queue:  queue.New(),
		Volume: cfg.Audio.Volume,
		Config: cfg,
		Source: src,
	}
}

func (p *Player) isPlayingInternal() bool {
	return p.stream != nil && p.stream.State() != gumbleffmpeg.StateStopped
}

func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isPlayingInternal()
}

func (p *Player) PlayCurrent() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Queue.IsEmpty() {
		log.Println("[Player] Queue is empty, nothing to play")
		return
	}

	if p.isPlayingInternal() {
		log.Println("[Player] Already playing, skipping PlayCurrent")
		return
	}

	path, title := p.Queue.Current()
	if path != "" {
		log.Printf("[Player] Playing current: %s", title)
		p.playInternal(path)
	}
}

func (p *Player) PlayPath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isPlayingInternal() {
		log.Println("[Player] Stopping current stream to play new path")
	}

	log.Printf("[Player] Playing: %s", path)
	p.playInternal(path)
}

func (p *Player) PlayLast() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isPlayingInternal() {
		return
	}

	path, ok := p.Queue.PlayLast()
	if ok {
		log.Printf("[Player] Playing last queued: %s", path)
		p.playInternal(path)
	}
}

func (p *Player) playInternal(path string) {
	p.playInternalAt(path, 0, true)
}

func (p *Player) playInternalAt(path string, offset time.Duration, announce bool) {
	if p.radioMode {
		p.stopRadioInternalLocked()
	}
	if p.stream != nil {
		log.Println("[Player] Interrupting current stream for:", path)
		oldStream := p.stream
		p.stream = nil
		oldStream.Stop()
	}

	p.paused = false
	p.pausedAt = 0
	p.streamStartOffset = offset

	var err error
	if strings.HasPrefix(path, "http") {
		err = p.playURL(path, offset)
	} else {
		err = p.playFile(path, offset)
	}

	if err != nil {
		log.Printf("[Player] Playback error: %v", err)
		p.sendMessage("<b style=\"color:red\">Error:</b> " + utils.EscapeHTML(err.Error()))
		return
	}

	p.wantsToStop = false
	nowPlaying := p.getNowPlaying()
	p.SetComment(nowPlaying)
	if announce {
		p.sendMessage(nowPlaying)
	}

	log.Printf("[Player] Now playing: %s", path)
	go p.waitForStop()
}

func (p *Player) playURL(url string, offset time.Duration) error {
	src := p.Source.GetStream(url)
	if src == nil {
		return fmt.Errorf("no source found for URL: %s", url)
	}
	p.stream = gumbleffmpeg.New(p.Client, src)
	p.stream.Offset = offset
	p.stream.Volume = p.Volume
	return p.stream.Play()
}

func (p *Player) playFile(path string, offset time.Duration) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	p.stream = gumbleffmpeg.New(p.Client, gumbleffmpeg.SourceFile(path))
	p.stream.Offset = offset
	p.stream.Volume = p.Volume
	return p.stream.Play()
}

func (p *Player) waitForStop() {
	p.mu.Lock()
	currentStream := p.stream
	isRadio := p.radioMode
	p.mu.Unlock()

	if currentStream == nil {
		return
	}

	currentStream.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stream != currentStream {
		return
	}

	if isRadio {
		log.Println("[Player] Radio stream ended")
		p.stopRadioInternalLocked()
		return
	}

	if p.wantsToStop {
		log.Println("[Player] Stream stopped (wantsToStop)")
		p.stopInternal(true)
		return
	}

	if p.Queue.HasNext() {
		nextPath, ok := p.Queue.Next()
		if ok {
			log.Printf("[Player] Auto-advancing to next: %s", nextPath)
			p.playInternal(nextPath)
			return
		}
	}

	log.Println("[Player] Queue exhausted, stopping")
	p.stopInternal(true)
}

func (p *Player) Stop(wantsToStop bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.radioMode {
		p.stopRadioInternalLocked()
		return
	}
	p.stopInternal(wantsToStop)
}

func (p *Player) stopInternal(wantsToStop bool) {
	p.wantsToStop = wantsToStop
	p.paused = false
	p.pausedAt = 0
	if p.stream != nil {
		log.Println("[Player] Stopping stream")
		oldStream := p.stream
		p.stream = nil
		oldStream.Stop()
		p.SetComment("Not Playing.")
		oldStream.Wait()
	}
}

func (p *Player) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.radioMode {
		return fmt.Errorf("cannot pause radio")
	}
	if p.paused {
		return fmt.Errorf("already paused")
	}
	if p.stream == nil {
		return fmt.Errorf("nothing is playing")
	}

	pausedAt := p.streamStartOffset + p.stream.Elapsed()
	p.stopInternal(false)
	p.paused = true
	p.pausedAt = pausedAt
	p.SetComment("Paused at " + FormatDuration(pausedAt))
	log.Printf("[Player] Paused at %s", FormatDuration(pausedAt))
	return nil
}

func (p *Player) Resume() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.paused {
		return false
	}

	path, title := p.Queue.Current()
	if path == "" {
		p.paused = false
		p.pausedAt = 0
		return false
	}

	log.Printf("[Player] Resuming %s from %s", title, FormatDuration(p.pausedAt))
	p.playInternalAt(path, p.pausedAt, true)
	return true
}

// Seek jumps the current track to the given offset. It does nothing on radio
// streams. When paused it only moves the pause point; when playing it restarts
// the stream at the new offset. Returns false if there is nothing to seek.
func (p *Player) Seek(offset time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.radioMode {
		return false
	}

	path, _ := p.Queue.Current()
	if path == "" {
		return false
	}

	if p.paused {
		p.pausedAt = offset
		p.SetComment("Paused at " + FormatDuration(offset))
		log.Printf("[Player] Seeked (paused) to %s", FormatDuration(offset))
		return true
	}

	log.Printf("[Player] Seeking to %s", FormatDuration(offset))
	p.stopInternal(false)
	p.playInternalAt(path, offset, false)
	return true
}

func (p *Player) Skip(amount int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.radioMode {
		p.stopRadioInternalLocked()
		return
	}

	if p.Queue.HasNext() {
		nextPath, ok := p.Queue.Skip(amount)
		if ok {
			log.Printf("[Player] Skipping %d track(s) to: %s", amount, nextPath)
			p.stopInternal(true)
			p.playInternal(nextPath)
			return
		}
	}

	log.Println("[Player] No more tracks, stopping")
	p.stopInternal(true)
	p.sendMessage("End of queue.")
}

func (p *Player) SetVolume(value float32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Volume = value
	p.Config.Audio.Volume = value
	if p.stream != nil {
		p.stream.Volume = value
	}
	log.Printf("[Player] Volume set to %.0f%%", value*100)
}

func (p *Player) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.radioMode {
		p.stopRadioInternalLocked()
	}
	p.stopInternal(true)
	p.Queue.Clear()
	p.sendMessage("<b>Bot state has been reset.</b>")
	log.Println("[Player] State reset")
}

func (p *Player) getNowPlaying() string {
	if p.radioMode {
		title := utils.EscapeHTML(p.radioTitle)
		artImg := p.radioArtImg

		if title == "" {
			title = "Live Radio"
		}

		output := "<table cellpadding=\"2\" cellspacing=\"0\"><tr>"
		output += "<td>" + artImg + "</td>"
		output += "<td>&nbsp;<b><u><small>Now Playing (Radio)</small></u></b><br>"
		output += "<font size=\"+1\">" + title + "</font>"
		output += "</td></tr></table>"
		return output
	}

	path, title := p.Queue.Current()
	if path == "" {
		return "Not Playing"
	}

	var artImg string
	if strings.HasPrefix(path, "http") {
		artImg = p.Source.GetThumbnail(path)
	}

	bitrate := utils.GetBitrate(path)
	queueCount := p.Queue.Count()

	output := "<table cellpadding=\"2\" cellspacing=\"0\"><tr>"
	output += "<td>" + artImg + "</td>"
	output += "<td>&nbsp;<b><u><small>Now Playing</small></u></b><br>"
	output += "<font size=\"+1\">" + utils.EscapeHTML(title) + "</font>"
	if bitrate != "" {
		output += "<br>Bitrate: " + bitrate
	}
	if p.Config.Bot.ShowDuration {
		if dur := p.Queue.CurrentDuration(); dur > 0 {
			output += "<br>Duration: " + FormatDuration(dur)
		}
	}
	output += fmt.Sprintf("<br>%d queued", queueCount)
	output += "</td></tr></table>"

	return output
}

func (p *Player) sendMessage(msg string) {
	if p.Client != nil && p.Client.Self != nil && p.Client.Self.Channel != nil {
		p.Client.Self.Channel.Send(msg, false)
	}
}

func (p *Player) SendReply(msg, sender string, isPrivate bool) {
	if isPrivate {
		if user := p.Client.Users.Find(sender); user != nil {
			user.Send(msg)
			return
		}
	}
	p.sendMessage(msg)
}

func (p *Player) SetComment(msg string) {
	if p.Client != nil && p.Client.Self != nil {
		p.Client.Self.SetComment(msg)
	}
}

func (p *Player) SummonToUser(username string) bool {
	user := p.Client.Users.Find(username)
	if user == nil {
		return false
	}
	p.Client.Self.Move(user.Channel)
	log.Printf("[Player] Summoned to %s's channel", username)
	return true
}

func (p *Player) MoveToChannel(channelName string) bool {
	channel := p.Client.Channels.Find(channelName)
	if channel == nil {
		return false
	}
	p.Client.Self.Move(channel)
	log.Printf("[Player] Moved to channel: %s", channelName)
	return true
}

func (p *Player) IsRadioMode() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.radioMode
}

func (p *Player) SetRadioMode(active bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.radioMode = active
}

func (p *Player) PlayRadio(streamURL, title, artImg string, pollInterval time.Duration, onUpdate func(radio.Metadata)) {
	p.mu.Lock()

	if p.stream != nil {
		log.Println("[Player] Stopping current stream for radio")
		oldStream := p.stream
		p.stream = nil
		oldStream.Stop()
	}

	p.radioMode = true
	p.radioTitle = title
	p.radioArtImg = artImg
	p.Queue.Clear()
	p.mu.Unlock()

	p.playRadioInternal(streamURL, pollInterval, onUpdate)
}

func (p *Player) playRadioInternal(streamURL string, pollInterval time.Duration, onUpdate func(radio.Metadata)) {
	p.mu.Lock()
	if p.stream != nil {
		oldStream := p.stream
		p.stream = nil
		oldStream.Stop()
	}

	src := p.Source.GetStream(streamURL)
	if src == nil {
		p.mu.Unlock()
		log.Printf("[Player] No source found for radio URL: %s", streamURL)
		p.sendMessage("<b style=\"color:red\">Error:</b> No source found for radio stream")
		return
	}
	p.stream = gumbleffmpeg.New(p.Client, src)
	p.stream.Volume = p.Volume
	err := p.stream.Play()
	if err != nil {
		p.mu.Unlock()
		log.Printf("[Player] Radio playback error: %v", err)
		p.sendMessage("<b style=\"color:red\">Error:</b> " + utils.EscapeHTML(err.Error()))
		return
	}
	p.wantsToStop = false
	p.mu.Unlock()

	if pollInterval > 0 {
		p.mu.Lock()
		if p.radioMetadata != nil {
			p.radioMetadata.Stop()
			p.radioMetadata = nil
		}
		manager := radio.NewMetadataManager()
		p.radioMetadata = manager
		p.mu.Unlock()

		manager.StartPolling(streamURL, pollInterval, func(meta radio.Metadata) {
			p.mu.Lock()
			if p.radioMetadata != manager {
				p.mu.Unlock()
				return
			}
			p.radioTitle = meta.Title
			if meta.ArtURL != "" {
				p.radioArtImg = p.FetchRadioArt(meta.ArtURL)
			}
			p.mu.Unlock()

			if onUpdate != nil {
				onUpdate(meta)
			}
		})
	}

	nowPlaying := p.getRadioNowPlaying()
	p.sendMessage(nowPlaying)
	p.SetComment(nowPlaying)

	log.Printf("[Player] Now playing radio: %s", streamURL)
	go p.waitForRadioStop()
}

func (p *Player) FetchRadioArt(artURL string) string {
	if artURL == "" {
		return ""
	}

	log.Printf("[Player] Fetching radio artwork: %s", artURL)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(artURL)
	if err != nil {
		log.Printf("[Player] Radio artwork download failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	img, err := utils.DecodeImage(imgData)
	if err != nil {
		log.Printf("[Player] Radio artwork decode failed: %v", err)
		return ""
	}

	resized := utils.ResizeImage(img, 100, 100)

	return utils.EncodeImageDataURI(resized, 4850)
}

func (p *Player) waitForRadioStop() {
	p.mu.Lock()
	currentStream := p.stream
	p.mu.Unlock()

	if currentStream == nil {
		return
	}

	currentStream.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stream != currentStream {
		return
	}

	if p.wantsToStop {
		log.Println("[Player] Radio stream stopped (wantsToStop)")
		p.stopRadioInternalLocked()
		return
	}

	log.Println("[Player] Radio stream ended unexpectedly, stopping")
	p.stopRadioInternalLocked()
}

func (p *Player) StopRadio() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopRadioInternalLocked()
}

func (p *Player) stopRadioInternalLocked() {
	p.radioMode = false
	if p.radioMetadata != nil {
		p.radioMetadata.Stop()
		p.radioMetadata = nil
	}
	p.radioTitle = ""
	p.radioArtImg = ""
	if p.stream != nil {
		log.Println("[Player] Stopping radio stream")
		oldStream := p.stream
		p.stream = nil
		oldStream.Stop()
		p.SetComment("Not Playing.")
		oldStream.Wait()
	}
}

func (p *Player) getRadioNowPlaying() string {
	p.mu.Lock()
	title := utils.EscapeHTML(p.radioTitle)
	artImg := p.radioArtImg
	p.mu.Unlock()

	if title == "" {
		title = "Live Radio"
	}

	output := "<table cellpadding=\"2\" cellspacing=\"0\"><tr>"
	output += "<td>" + artImg + "</td>"
	output += "<td>&nbsp;<b><u><small>Now Playing (Radio)</small></u></b><br>"
	output += "<font size=\"+1\">" + title + "</font>"
	output += "</td></tr></table>"

	return output
}

func FormatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	hours := secs / 3600
	mins := (secs % 3600) / 60
	secs = secs % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

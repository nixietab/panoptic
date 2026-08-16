package commands

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/nixietab/panoptic/config"
	"github.com/nixietab/panoptic/player"
)

type CommandFunc func(p *player.Player, sender string, isPrivate bool, arg string)

var commandMap = map[string]CommandFunc{}
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

func RegisterCommand(name string, handler CommandFunc) {
	commandMap[strings.ToLower(name)] = handler
}

func RegisterJellyfinCommands(count int) {
	if count <= 0 {
		return
	}
	RegisterCommand("j", makeJellyfinCmd(0))
	RegisterCommand("jellyfin", makeJellyfinCmd(0))
	for i := 1; i < count; i++ {
		name := fmt.Sprintf("j%d", i+1)
		RegisterCommand(name, makeJellyfinCmd(i))
	}
}

func RegisterRadioCommands() {
	RegisterCommand("radio", cmdRadio)
	RegisterCommand("r", cmdRadio)
}

func IsCommand(message string, isPrivate bool, username string, cfg *config.Config) bool {
	message = strings.TrimSpace(message)
	if strings.HasPrefix(message, cfg.Bot.Prefix) {
		return true
	}
	if cfg.Bot.Prefix != "." && strings.HasPrefix(message, ".") {
		return true
	}
	if strings.HasPrefix(message, username+" ") || strings.HasPrefix(message, username+":") {
		return true
	}
	if isPrivate {
		return true
	}
	return false
}

func Dispatch(p *player.Player, msg string, isPrivate bool, sender string) {
	command, arg := parseCommand(msg, p.Client.Self.Name, p.Config)
	log.Printf("[Command] %s -> cmd=%s arg=%q", sender, command, arg)

	if handler, ok := commandMap[command]; ok {
		handler(p, sender, isPrivate, arg)
	} else {
		log.Printf("[Command] Unknown command: %s", command)
	}
}

func parseCommand(msg string, username string, cfg *config.Config) (string, string) {
	msg = strings.TrimSpace(msg)
	msg = htmlTagRegex.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)

	if strings.HasPrefix(msg, cfg.Bot.Prefix) {
		msg = msg[len(cfg.Bot.Prefix):]
	} else if cfg.Bot.Prefix != "." && strings.HasPrefix(msg, ".") {
		msg = msg[1:]
	} else if strings.HasPrefix(msg, username+" ") {
		msg = msg[len(username)+1:]
	} else if strings.HasPrefix(msg, username+":") {
		msg = msg[len(username)+1:]
	}

	parts := strings.SplitN(msg, " ", 2)
	command := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	return command, arg
}

func init() {
	RegisterCommand("play", cmdPlay)
	RegisterCommand("p", cmdPlay)
	RegisterCommand("pause", cmdPause)
	RegisterCommand("pa", cmdPause)
	RegisterCommand("skip", cmdSkip)
	RegisterCommand("s", cmdSkip)
	RegisterCommand("seek", cmdSeek)
	RegisterCommand("sk", cmdSeek)
	RegisterCommand("stop", cmdStop)
	RegisterCommand("list", cmdList)
	RegisterCommand("queue", cmdList)
	RegisterCommand("vol", cmdVol)
	RegisterCommand("help", cmdHelp)
	RegisterCommand("summon", cmdSummon)
	RegisterCommand("about", cmdAbout)
}

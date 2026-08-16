# panoptic
A self-hosted music bot for mumble


**panoptic** is a lightweight music bot for [Mumble](https://www.mumble.info/) that aims to be lightweight, configurable and easy to use.

 It can stream audio from YouTube, Jellyfin, and internet radio. Type a command in chat or send a private message to play music, manage queues, and control playback.


## Quick Start

### Prerequisites
* Go 1.22+
* [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) and [`ffmpeg`](https://ffmpeg.org/) installed on your system.

### Installation & Running

```sh
# Build the binary
go build -o panoptic

# Set up configuration
cp config.toml.example config.toml
$EDITOR config.toml

# Generate self-signed TLS certificates (if needed)
./gencerts.sh

# Run the bot
./panoptic
```

## License

Distributed under the **GNU Affero General Public License v3.0**. See [LICENSE](LICENSE).
package source

import (
	"github.com/nixietab/panoptic/config"
	"layeh.com/gumble/gumbleffmpeg"
)

// Source is an audio backend that can stream URLs and provide metadata.
type Source interface {
	GetStream(url string) gumbleffmpeg.Source
	GetTitle(url string) string
	GetThumbnail(url string) string
	IsWhitelisted(url string) bool
}

// Registry routes URLs to the first Source that claims them.
type Registry struct {
	sources   []Source
	whitelist *config.WhitelistConfig
}

func NewRegistry(wl *config.WhitelistConfig) *Registry {
	return &Registry{
		sources:   make([]Source, 0),
		whitelist: wl,
	}
}

func (r *Registry) Register(src Source) {
	r.sources = append(r.sources, src)
}

func (r *Registry) GetStream(url string) gumbleffmpeg.Source {
	for _, src := range r.sources {
		if src.IsWhitelisted(url) {
			return src.GetStream(url)
		}
	}
	return nil
}

func (r *Registry) GetTitle(url string) string {
	for _, src := range r.sources {
		if src.IsWhitelisted(url) {
			return src.GetTitle(url)
		}
	}
	return url
}

func (r *Registry) GetThumbnail(url string) string {
	for _, src := range r.sources {
		if src.IsWhitelisted(url) {
			return src.GetThumbnail(url)
		}
	}
	return ""
}

func (r *Registry) IsWhitelisted(url string) bool {
	if r.whitelist != nil && !r.whitelist.Enabled {
		return true
	}
	if r.whitelist != nil && r.whitelist.Enabled && !r.whitelist.IsAllowed(url) {
		return false
	}
	for _, src := range r.sources {
		if src.IsWhitelisted(url) {
			return true
		}
	}
	return false
}

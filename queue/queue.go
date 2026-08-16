package queue

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const QueueFile = "queue.json"

type Track struct {
	Path     string        `json:"path"`
	Title    string        `json:"title"`
	Duration time.Duration `json:"duration"`
}

type Queue struct {
	tracks    []Track
	position  int
	mu        sync.RWMutex
}

func New() *Queue {
	return &Queue{
		tracks:   make([]Track, 0),
		position: 0,
	}
}

func (q *Queue) Add(path, title string, duration time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = append(q.tracks, Track{Path: path, Title: title, Duration: duration})
}

func (q *Queue) PlayLast() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tracks) == 0 {
		return "", false
	}
	q.position = len(q.tracks) - 1
	return q.tracks[q.position].Path, true
}

func (q *Queue) AddNext(path, title string, duration time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 || q.position >= len(q.tracks)-1 {
		q.tracks = append(q.tracks, Track{Path: path, Title: title, Duration: duration})
		return
	}

	newTracks := make([]Track, 0, len(q.tracks)+1)
	newTracks = append(newTracks, q.tracks[:q.position+1]...)
	newTracks = append(newTracks, Track{Path: path, Title: title, Duration: duration})
	newTracks = append(newTracks, q.tracks[q.position+1:]...)
	q.tracks = newTracks
}

func (q *Queue) Next() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.position >= len(q.tracks)-1 {
		return "", false
	}
	q.position++
	return q.tracks[q.position].Path, true
}

func (q *Queue) Skip(amount int) (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	newPos := q.position + amount
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= len(q.tracks) {
		return "", false
	}
	q.position = newPos
	return q.tracks[q.position].Path, true
}

func (q *Queue) Current() (string, string) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.position < 0 || q.position >= len(q.tracks) {
		return "", ""
	}
	track := q.tracks[q.position]
	return track.Path, track.Title
}

func (q *Queue) CurrentDuration() time.Duration {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.position < 0 || q.position >= len(q.tracks) {
		return 0
	}
	return q.tracks[q.position].Duration
}

func (q *Queue) PeekNext() string {
	q.mu.RLock()
	defer q.mu.RUnlock()

	nextPos := q.position + 1
	if nextPos >= len(q.tracks) {
		return ""
	}
	return q.tracks[nextPos].Title
}

func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = make([]Track, 0)
	q.position = 0
}

func (q *Queue) List() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()

	list := make([]string, 0, len(q.tracks)-q.position)
	for i := q.position; i < len(q.tracks); i++ {
		list = append(list, q.tracks[i].Title)
	}
	return list
}

func (q *Queue) Count() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracks) - q.position
}

func (q *Queue) IsEmpty() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracks) == 0 || q.position >= len(q.tracks)
}

func (q *Queue) HasNext() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.position < len(q.tracks)-1
}

func (q *Queue) CurrentPosition() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.position
}

func (q *Queue) TotalTracks() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracks)
}

func (q *Queue) Save() {
	q.mu.RLock()
	remaining := q.tracks[q.position:]
	q.mu.RUnlock()

	if len(remaining) == 0 {
		return
	}

	data, err := json.MarshalIndent(remaining, "", "  ")
	if err != nil {
		log.Println("Failed to marshal queue:", err)
		return
	}

	if err := os.WriteFile(QueueFile, data, 0644); err != nil {
		log.Println("Failed to save queue:", err)
	}
}

func (q *Queue) Load() {
	file, err := os.ReadFile(QueueFile)
	if err != nil {
		return
	}

	var tracks []Track
	if err := json.Unmarshal(file, &tracks); err != nil {
		log.Println("Failed to load queue:", err)
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = tracks
	q.position = 0
	log.Println("Loaded", len(tracks), "tracks from queue file")
}

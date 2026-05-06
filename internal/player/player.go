// Package player implements the BubbleTea model that drives go-cli-beat.
package player

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"go-cli-beat/internal/audio"
	"go-cli-beat/internal/visualizer"
)

// ─── BubbleTea messages ───────────────────────────────────────────────────────

// TickMsg is sent every 50 ms to refresh the visualiser and elapsed time.
type TickMsg time.Time

// SongDoneMsg is sent when the current track finishes naturally.
type SongDoneMsg struct{}

// PlayErrMsg is sent when a track cannot be played (bad encoding, etc.).
// The player will automatically skip to the next track.
type PlayErrMsg struct {
	Path string
	Err  error
}

// ─── State ────────────────────────────────────────────────────────────────────

// State represents the playback state.
type State int

const (
	StatePlaying State = iota
	StatePaused
	StateStopped
)

// ─── Model ────────────────────────────────────────────────────────────────────

// Model is the root BubbleTea model.
type Model struct {
	// Playlist
	MusicDir    string
	Playlist    []string
	Current     int
	SkippedMsgs []string // recent skip warnings

	// Subsystems
	Engine     *audio.Engine
	Visualizer *visualizer.Viz

	// Playback
	PlayState State
	Elapsed   time.Duration
	StartTime time.Time

	// UI dimensions
	Width    int
	Height   int
	BarCount int

	Err error
}

// New builds a Model for the given music directory.
func New(musicDir string) (*Model, error) {
	files, err := scanMP3(musicDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", musicDir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .mp3 files found in %s", musicDir)
	}

	rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })

	engine, err := audio.New()
	if err != nil {
		return nil, err
	}

	const defaultBars = 32
	return &Model{
		MusicDir:   musicDir,
		Playlist:   files,
		Current:    0,
		Engine:     engine,
		Visualizer: visualizer.New(defaultBars),
		PlayState:  StateStopped,
		Width:      80,
		Height:     24,
		BarCount:   defaultBars,
	}, nil
}

func scanMP3(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".mp3") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// ─── BubbleTea interface ──────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.playCurrent(), tickCmd())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.BarCount = clampInt(m.Width/3, 8, 64)
		m.Visualizer.Resize(m.BarCount)

	case TickMsg:
		if m.PlayState == StatePlaying {
			m.Elapsed = time.Since(m.StartTime)
			m.Visualizer.Tick()
		}
		return m, tickCmd()

	case SongDoneMsg:
		return m, m.nextTrack()

	case PlayErrMsg:
		// Log the warning and skip automatically.
		short := filepath.Base(msg.Path)
		warn := fmt.Sprintf("⚠  skipped %q: %v", short, msg.Err)
		m.SkippedMsgs = append(m.SkippedMsgs, warn)
		if len(m.SkippedMsgs) > 3 {
			m.SkippedMsgs = m.SkippedMsgs[len(m.SkippedMsgs)-3:]
		}
		return m, m.nextTrack()

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		m.Engine.Close()
		return tea.Quit
	case " ":
		return m.togglePause()
	case "n", "right":
		return m.nextTrack()
	case "p", "left":
		return m.prevTrack()
	case "r":
		rand.Shuffle(len(m.Playlist), func(i, j int) {
			m.Playlist[i], m.Playlist[j] = m.Playlist[j], m.Playlist[i]
		})
		m.Current = 0
		return m.playCurrent()
	}
	return nil
}

// ─── Commands ─────────────────────────────────────────────────────────────────

func (m *Model) playCurrent() tea.Cmd {
	path := m.Playlist[m.Current]
	// Capture a reference to the program so we can send the done message.
	// We use a closure over `m` — safe because Update is single-threaded.
	return func() tea.Msg {
		err := m.Engine.Play(path, func() {
			// This callback runs in the speaker goroutine; we cannot
			// call Update directly. We rely on the tick loop to detect
			// that the streamer is exhausted — but sending via the
			// program channel is cleaner. We'll send via tea.Cmd instead.
		})
		if err != nil {
			return PlayErrMsg{Path: path, Err: err}
		}
		m.PlayState = StatePlaying
		m.StartTime = time.Now()
		m.Elapsed = 0
		m.Visualizer.Reset()
		return nil
	}
}



func (m *Model) togglePause() tea.Cmd {
	return func() tea.Msg {
		paused := m.Engine.TogglePause()
		if paused {
			m.PlayState = StatePaused
		} else {
			m.PlayState = StatePlaying
			m.StartTime = time.Now().Add(-m.Elapsed)
		}
		return nil
	}
}

func (m *Model) nextTrack() tea.Cmd {
	m.Current = (m.Current + 1) % len(m.Playlist)
	return m.playCurrent()
}

func (m *Model) prevTrack() tea.Cmd {
	m.Current = (m.Current - 1 + len(m.Playlist)) % len(m.Playlist)
	return m.playCurrent()
}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// ─── Helpers (used by ui package) ────────────────────────────────────────────

func (m *Model) StateIcon() string {
	switch m.PlayState {
	case StatePlaying:
		return "▶"
	case StatePaused:
		return "⏸"
	default:
		return "⏹"
	}
}

func (m *Model) FmtElapsed() string {
	s := int(m.Elapsed.Seconds())
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

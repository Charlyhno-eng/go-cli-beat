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

// TickMsg is sent every 50 ms to refresh the animation and elapsed time.
type TickMsg time.Time

// TrackStartedMsg confirms that the decoder and speaker accepted a track.
type TrackStartedMsg struct{}

// PauseToggledMsg carries the result of changing the speaker control.
type PauseToggledMsg struct {
	Paused  bool
	Changed bool
}

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
	Width               int
	Height              int
	Err                 error
	ConsecutiveFailures int
}

// New builds a Model for the given music directory.
func New(musicDir string) (*Model, error) {
	files, err := scanAudio(musicDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", musicDir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no audio files found in %s", musicDir)
	}

	rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })

	engine, err := audio.New()
	if err != nil {
		return nil, err
	}

	return &Model{
		MusicDir:   musicDir,
		Playlist:   files,
		Current:    0,
		Engine:     engine,
		Visualizer: visualizer.New(0),
		PlayState:  StateStopped,
		Width:      80,
		Height:     24,
	}, nil
}

func scanAudio(dir string) ([]string, error) {
	// ffmpeg can decode many formats; this list only avoids adding unrelated
	// files to the playlist. Walking recursively also supports artist/album
	// folder layouts.
	supported := map[string]bool{
		".aac": true, ".aif": true, ".aiff": true, ".flac": true,
		".m4a": true, ".mka": true, ".mp3": true, ".mpga": true,
		".ogg": true, ".oga": true, ".opus": true, ".wav": true,
		".webm": true, ".wma": true,
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if supported[strings.ToLower(filepath.Ext(entry.Name()))] {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
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

	case TickMsg:
		if m.PlayState == StatePlaying {
			m.Elapsed = time.Since(m.StartTime)
			m.Visualizer.Tick()
			if completed, err := m.Engine.ConsumeCompletion(); completed {
				if err != nil {
					return m, tea.Batch(m.skipTrack(m.Playlist[m.Current], err), tickCmd())
				}
				return m, tea.Batch(m.nextTrack(), tickCmd())
			}
		}
		return m, tickCmd()

	case TrackStartedMsg:
		m.PlayState = StatePlaying
		m.StartTime = time.Now()
		m.Elapsed = 0
		m.ConsecutiveFailures = 0
		m.Visualizer.Reset()
		return m, nil

	case PauseToggledMsg:
		if !msg.Changed {
			return m, nil
		}
		if msg.Paused {
			m.PlayState = StatePaused
		} else {
			m.PlayState = StatePlaying
			m.StartTime = time.Now().Add(-m.Elapsed)
		}
		return m, nil

	case PlayErrMsg:
		return m, m.skipTrack(msg.Path, msg.Err)

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
	return func() tea.Msg {
		err := m.Engine.Play(path, m.Visualizer.Observe)
		if err != nil {
			return PlayErrMsg{Path: path, Err: err}
		}
		return TrackStartedMsg{}
	}
}

func (m *Model) skipTrack(path string, err error) tea.Cmd {
	short := filepath.Base(path)
	warn := fmt.Sprintf("⚠ skipped %q: %v", short, err)
	m.SkippedMsgs = append(m.SkippedMsgs, warn)
	if len(m.SkippedMsgs) > 3 {
		m.SkippedMsgs = m.SkippedMsgs[len(m.SkippedMsgs)-3:]
	}
	m.ConsecutiveFailures++
	if m.ConsecutiveFailures >= len(m.Playlist) {
		m.PlayState = StateStopped
		m.SkippedMsgs = append(m.SkippedMsgs, "⚠ aucun fichier de la playlist ne peut être décodé")
		return nil
	}
	return m.nextTrack()
}

func (m *Model) togglePause() tea.Cmd {
	return func() tea.Msg {
		paused, changed := m.Engine.TogglePause()
		return PauseToggledMsg{Paused: paused, Changed: changed}
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

package player

import (
	"path/filepath"
	"strings"

	"go-cli-beat/internal/ui"
)

// View implements tea.Model. It converts the Model into a ui.ViewData snapshot
// and delegates all rendering to the ui package. This keeps ui free of any
// dependency on the player package, breaking the import cycle.
func (m *Model) View() string {
	return ui.Render(m.toViewData())
}

func (m *Model) toViewData() ui.ViewData {
	d := ui.ViewData{
		Err:         m.Err,
		StateIcon:   m.StateIcon(),
		Elapsed:     m.FmtElapsed(),
		TrackIndex:  m.Current + 1,
		TrackTotal:  len(m.Playlist),
		SkippedMsgs: m.SkippedMsgs,
		Width:       m.Width,
		Height:      m.Height,
		Paused:      m.PlayState == StatePaused,
	}
	d.Artist, d.Title = m.CurrentTrack()

	// The renderer receives only an immutable snapshot of the audio-driven
	// motion state; samples themselves stay in the audio goroutine.
	motion := m.Visualizer.Snapshot()
	d.MotionEnergy = motion.Energy
	d.MotionBeat = motion.Beat
	d.MotionFrame = motion.Frame
	d.PulseAge = motion.PulseAge
	d.PulseStrength = motion.PulseStrength

	// Keep at most four entries visible. On a conventional 80×24 terminal,
	// reserve one row for the full centred experience and show three entries.
	playlistWindowSize := 4
	if m.Height <= 24 {
		playlistWindowSize = 3
	}
	total := len(m.Playlist)
	if total > 0 {
		start := m.Current - (playlistWindowSize-1)/2
		if start < 0 {
			start = 0
		}
		end := start + playlistWindowSize
		if end > total {
			end = total
			if end-playlistWindowSize > 0 {
				start = end - playlistWindowSize
			} else {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			artist, title := trackDetails(m.Playlist[i])
			d.PlaylistWindow = append(d.PlaylistWindow, ui.PlaylistEntry{
				Artist:    artist,
				Title:     title,
				IsCurrent: i == m.Current,
			})
		}
	}

	return d
}

// ─── Display helpers ──────────────────────────────────────────────────────────

func (m *Model) CurrentTrack() (string, string) {
	if len(m.Playlist) == 0 {
		return "Artiste inconnu", "—"
	}
	return trackDetails(m.Playlist[m.Current])
}

// trackDetails derives display metadata from the filename so that tagless music
// stays first-class. It splits only on a spaced dash to keep hyphenated titles.
func trackDetails(path string) (artist, title string) {
	base := filepath.Base(path)
	name := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	for _, separator := range []string{" - ", " — ", " – "} {
		if before, after, found := strings.Cut(name, separator); found {
			artist = strings.TrimSpace(before)
			title = strings.TrimSpace(after)
			if artist == "" {
				artist = "Artiste inconnu"
			}
			if title == "" {
				title = "Titre inconnu"
			}
			return artist, title
		}
	}
	if name == "" {
		name = "Titre inconnu"
	}
	return "Artiste inconnu", name
}

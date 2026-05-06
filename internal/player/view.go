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
		TrackName:   m.CurrentName(),
		Elapsed:     m.FmtElapsed(),
		TrackIndex:  m.Current + 1,
		TrackTotal:  len(m.Playlist),
		SkippedMsgs: m.SkippedMsgs,
		Width:       m.Width,
		Height:      m.Height,
	}

	// Bar heights — only when playing or paused.
	if m.PlayState != StateStopped {
		d.BarHeights = m.Visualizer.Heights()
	}

	// Playlist window: up to 5 entries centred on m.Current.
	total := len(m.Playlist)
	if total > 0 {
		start := m.Current - 2
		if start < 0 {
			start = 0
		}
		end := start + 5
		if end > total {
			end = total
			if end-5 > 0 {
				start = end - 5
			} else {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			d.PlaylistWindow = append(d.PlaylistWindow, ui.PlaylistEntry{
				Path:      m.Playlist[i],
				IsCurrent: i == m.Current,
			})
		}
	}

	return d
}

// ─── Display helpers ──────────────────────────────────────────────────────────

func (m *Model) CurrentName() string {
	if len(m.Playlist) == 0 {
		return "—"
	}
	base := filepath.Base(m.Playlist[m.Current])
	return strings.TrimSuffix(base, filepath.Ext(base))
}

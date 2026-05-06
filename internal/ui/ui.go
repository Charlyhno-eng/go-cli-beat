// Package ui implements the BubbleTea View() for go-cli-beat.
// It is deliberately free of any import from sibling packages; the player
// package builds a ViewData value and passes it here.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// ─── ViewData ─────────────────────────────────────────────────────────────────

// ViewData carries everything the renderer needs. The player package fills this
// struct in its View() method so that ui never needs to import player.
type ViewData struct {
	// Fatal error — if non-nil, only an error screen is shown.
	Err error

	// Now-playing info.
	StateIcon   string // ▶ ⏸ ⏹
	TrackName   string // display name (no extension)
	Elapsed     string // "MM:SS"
	TrackIndex  int    // 1-based
	TrackTotal  int

	// Visualizer bars [0..1], nil or empty → inactive.
	BarHeights []float64

	// Playlist window — pre-sliced by the caller.
	PlaylistWindow []PlaylistEntry

	// Warnings from skipped files.
	SkippedMsgs []string

	// Terminal dimensions.
	Width  int
	Height int
}

// PlaylistEntry is one row in the playlist window.
type PlaylistEntry struct {
	Path      string
	IsCurrent bool
}

// ─── Colour palette ───────────────────────────────────────────────────────────

var (
	clrAccent  = lipgloss.Color("#ff2d78")
	clrCyan    = lipgloss.Color("#00f5ff")
	clrPurple  = lipgloss.Color("#bf5fff")
	clrDim     = lipgloss.Color("#3a3a3a")
	clrText    = lipgloss.Color("#e0e0e0")
	clrSubtext = lipgloss.Color("#666666")
	clrBarBass = lipgloss.Color("#ff2d78") // pink   — bass
	clrBarMid  = lipgloss.Color("#bf5fff") // violet — mids
	clrBarHigh = lipgloss.Color("#00f5ff") // cyan   — highs
	clrBarPeak = lipgloss.Color("#ffffff") // white  — peak cells
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	sTitle   = lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	sSub     = lipgloss.NewStyle().Foreground(clrSubtext)
	sTrack   = lipgloss.NewStyle().Foreground(clrCyan).Bold(true)
	sState   = lipgloss.NewStyle().Foreground(clrPurple).Bold(true)
	sKey     = lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	sKeyDesc = lipgloss.NewStyle().Foreground(clrSubtext)
	sDim     = lipgloss.NewStyle().Foreground(clrDim)
	sElapsed = lipgloss.NewStyle().Foreground(clrText)
	sWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaa00"))
)

// Unicode block characters for fractional bar heights.
var blocks = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// ─── View entry point ─────────────────────────────────────────────────────────

// Render produces the full TUI string from a ViewData snapshot.
func Render(d ViewData) string {
	if d.Err != nil {
		return fmt.Sprintf("\n  ❌ Fatal error: %v\n\n  Press q to quit.\n", d.Err)
	}

	lines := []string{
		"",
		renderHeader(),
		"",
		renderNowPlaying(d),
		"",
		renderVisualizer(d),
		"",
		renderPlaylist(d),
		renderWarnings(d),
		renderControls(),
		"",
	}
	return strings.Join(lines, "\n")
}

// ─── Header ───────────────────────────────────────────────────────────────────

func renderHeader() string {
	return "  " + sTitle.Render("◈ GO-CLI-BEAT") +
		sSub.Render("  BubbleTea · beep · cava-style visualizer")
}

// ─── Now playing ──────────────────────────────────────────────────────────────

func renderNowPlaying(d ViewData) string {
	maxW := max(d.Width-16, 10)
	icon := sState.Render(d.StateIcon)
	name := sTrack.Render(truncate(d.TrackName, maxW))
	idx := sSub.Render(fmt.Sprintf("%d/%d", d.TrackIndex, d.TrackTotal))
	elapsed := sElapsed.Render(d.Elapsed)
	return fmt.Sprintf("  %s  %s   %s   %s", icon, name, elapsed, idx)
}

// ─── Visualizer ───────────────────────────────────────────────────────────────

func renderVisualizer(d ViewData) string {
	if len(d.BarHeights) == 0 {
		return sDim.Render("  [visualizer inactive]")
	}

	heights := d.BarHeights
	count := len(heights)
	maxLines := clampInt(d.Height/3, 4, 18)

	// Build a 2D grid: rows[row][col] = rendered cell string.
	// row 0 = top, row maxLines-1 = bottom.
	grid := make([][]string, maxLines)
	for r := range grid {
		grid[r] = make([]string, count)
	}

	for i, h := range heights {
		// Total filled "eighths" out of maxLines*8.
		filled := int(h * float64(maxLines*8))

		for row := 0; row < maxLines; row++ {
			// Which row from the bottom does this grid row represent?
			rowFromBottom := maxLines - 1 - row
			lineStart := rowFromBottom * 8
			lineEnd := lineStart + 8

			var cell string
			switch {
			case filled >= lineEnd:
				// Fully filled row.
				cell = barStyle(i, count, rowFromBottom, maxLines).Render("█")
			case filled > lineStart:
				// Partially filled row.
				partial := filled - lineStart
				if partial >= len(blocks) {
					partial = len(blocks) - 1
				}
				cell = barStyle(i, count, rowFromBottom, maxLines).Render(blocks[partial])
			default:
				cell = sDim.Render("░")
			}
			grid[row][i] = cell
		}
	}

	var sb strings.Builder
	for _, row := range grid {
		sb.WriteString("  ")
		for _, cell := range row {
			sb.WriteString(cell)
			sb.WriteString(" ")
		}
		sb.WriteString("\n")
	}
	// Baseline rule.
	sb.WriteString("  ")
	sb.WriteString(sDim.Render(strings.Repeat("─", count*2-1)))
	return sb.String()
}

// barStyle picks a colour based on spectral position and height.
func barStyle(col, total, rowFromBottom, maxRows int) lipgloss.Style {
	pos := float64(col) / float64(max(total-1, 1))
	heightRatio := float64(rowFromBottom) / float64(max(maxRows-1, 1))

	if heightRatio > 0.82 {
		return lipgloss.NewStyle().Foreground(clrBarPeak)
	}
	switch {
	case pos < 0.35:
		return lipgloss.NewStyle().Foreground(clrBarBass)
	case pos < 0.70:
		return lipgloss.NewStyle().Foreground(clrBarMid)
	default:
		return lipgloss.NewStyle().Foreground(clrBarHigh)
	}
}

// ─── Playlist ─────────────────────────────────────────────────────────────────

func renderPlaylist(d ViewData) string {
	if len(d.PlaylistWindow) == 0 {
		return ""
	}
	maxW := max(d.Width-10, 10)

	var sb strings.Builder
	sb.WriteString(sSub.Render("  ── Playlist ──") + "\n")

	for _, entry := range d.PlaylistWindow {
		base := filepath.Base(entry.Path)
		name := truncate(strings.TrimSuffix(base, filepath.Ext(base)), maxW)
		if entry.IsCurrent {
			sb.WriteString("  " + sState.Render("▶ ") + sTrack.Render(name) + "\n")
		} else {
			sb.WriteString("  " + sKeyDesc.Render("  "+name) + "\n")
		}
	}
	return sb.String()
}

// ─── Warnings ─────────────────────────────────────────────────────────────────

func renderWarnings(d ViewData) string {
	if len(d.SkippedMsgs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, w := range d.SkippedMsgs {
		sb.WriteString("  " + sWarn.Render(w) + "\n")
	}
	return sb.String()
}

// ─── Controls ─────────────────────────────────────────────────────────────────

func renderControls() string {
	pairs := [][2]string{
		{"space", "pause/play"},
		{"→ / n", "next"},
		{"← / p", "prev"},
		{"r", "shuffle"},
		{"q", "quit"},
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, sKey.Render("["+p[0]+"]")+sKeyDesc.Render(" "+p[1]))
	}
	return "  " + strings.Join(parts, sSub.Render("  ·  "))
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func truncate(s string, maxW int) string {
	if utf8.RuneCountInString(s) <= maxW {
		return s
	}
	return string([]rune(s)[:maxW-1]) + "…"
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

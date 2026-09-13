// Package ui renders the cyberpunk terminal interface.
package ui

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// ViewData is the render-only snapshot built by the player package.
type ViewData struct {
	Err error

	StateIcon  string
	Artist     string
	Title      string
	Elapsed    string
	TrackIndex int
	TrackTotal int

	// Motion comes from the actual PCM stream, not a synthetic visualiser.
	MotionEnergy  float64
	MotionBeat    float64
	MotionFrame   uint64
	PulseAge      uint64
	PulseStrength float64
	Paused        bool

	PlaylistWindow []PlaylistEntry
	SkippedMsgs    []string
	Width          int
	Height         int
}

type PlaylistEntry struct {
	Artist    string
	Title     string
	IsCurrent bool
}

var (
	clrPink   = lipgloss.Color("#ff2d78")
	clrCyan   = lipgloss.Color("#00f5ff")
	clrViolet = lipgloss.Color("#bf5fff")
	clrText   = lipgloss.Color("#e9e6f0")
	clrSub    = lipgloss.Color("#8a8095")
	clrDim    = lipgloss.Color("#443c4c")
	clrWarn   = lipgloss.Color("#ffb000")

	sBrand  = lipgloss.NewStyle().Foreground(clrPink).Bold(true)
	sCyan   = lipgloss.NewStyle().Foreground(clrCyan).Bold(true)
	sViolet = lipgloss.NewStyle().Foreground(clrViolet).Bold(true)
	sText   = lipgloss.NewStyle().Foreground(clrText)
	sSub    = lipgloss.NewStyle().Foreground(clrSub)
	sDim    = lipgloss.NewStyle().Foreground(clrDim)
	sKey    = lipgloss.NewStyle().Foreground(clrPink).Bold(true)
	sWarn   = lipgloss.NewStyle().Foreground(clrWarn)
)

func Render(d ViewData) string {
	if d.Err != nil {
		return fmt.Sprintf("\n  %s %v\n", sWarn.Render("ERREUR"), d.Err)
	}

	// Keep the application compact on large terminals; the remaining space is
	// reserved for the animated ambient field in centerScene.
	scene := d
	scene.Width = sceneWidth(d.Width)
	parts := []string{
		renderBrand(scene.Width),
		"",
		renderHeader(scene),
		renderNowPlaying(scene),
		renderReactor(scene),
		renderPlaylist(scene),
		renderWarnings(scene),
		renderControls(),
	}
	return centerScene(strings.Join(parts, "\n"), d)
}

// renderBrand is terminal-native on purpose: unlike a reduced PNG it is crisp
// in every terminal and evokes the command/music logo placed before the name.
func renderBrand(width int) string {
	icon := []string{
		sCyan.Render("╭────") + sBrand.Render("─╮"),
		sCyan.Render("│") + sBrand.Render(" ▸ ") + sViolet.Render("♫") + sBrand.Render(" │"),
		sCyan.Render("│") + sViolet.Render("  _  ") + sBrand.Render("│"),
		sCyan.Render("╰══") + sViolet.Render("══") + sBrand.Render("═╯"),
		"  " + sCyan.Render("║") + " " + sBrand.Render("║") + "  ",
	}
	title := asciiTitle("GO CLI BEAT")
	lines := make([]string, len(title))
	for row := range title {
		line := icon[row] + "  " + sBrand.Render(title[row])
		lines[row] = centerStyled(line, width)
	}
	return strings.Join(lines, "\n")
}

func asciiTitle(text string) []string {
	font := map[rune][]string{
		'G': {" ███ ", "█    ", "█ ███", "█   █", " ███ "},
		'O': {" ███ ", "█   █", "█   █", "█   █", " ███ "},
		'C': {" ███ ", "█    ", "█    ", "█    ", " ███ "},
		'L': {"█    ", "█    ", "█    ", "█    ", "█████"},
		'I': {"███", " █ ", " █ ", " █ ", "███"},
		'B': {"████ ", "█   █", "████ ", "█   █", "████ "},
		'E': {"█████", "█    ", "████ ", "█    ", "█████"},
		'A': {" ███ ", "█   █", "█████", "█   █", "█   █"},
		'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
		' ': {"  ", "  ", "  ", "  ", "  "},
	}
	lines := make([]string, 5)
	for _, letter := range text {
		glyph, found := font[letter]
		if !found {
			glyph = font[' ']
		}
		for row := range lines {
			lines[row] += glyph[row] + " "
		}
	}
	return lines
}

func renderHeader(d ViewData) string {
	status := "LIVE"
	if d.Paused {
		status = "PAUSED"
	}
	return "  " + sBrand.Render("◈ GO CLI BEAT") +
		sSub.Render("   TERMINAL AUDIO") +
		"  " + sCyan.Render("["+status+"]")
}

func renderNowPlaying(d ViewData) string {
	maxWidth := max(d.Width-30, 16)
	artistLimit := max(maxWidth/3, 12)
	artist := sBrand.Render(truncate(d.Artist, artistLimit))
	title := sText.Bold(true).Render(truncate(d.Title, maxWidth-artistLimit))
	meta := sSub.Render(fmt.Sprintf("FILE %02d/%02d", d.TrackIndex, d.TrackTotal))
	return "  " + sViolet.Render(d.StateIcon) + "  " + artist + sDim.Render(" — ") + title + "  " +
		sCyan.Render(d.Elapsed) + "  " + meta
}

// renderReactor draws a PCM-powered light reactor. Its expanding rings are
// driven by beat transients, while particles become denser and travel faster as
// actual signal energy rises. There are deliberately no spectrum bars.
func renderReactor(d ViewData) string {
	inner := d.Width - 6
	if inner < 26 {
		return "  " + sDim.Render("[ terminal trop étroit pour l'animation ]")
	}
	if inner > 88 {
		inner = 88
	}

	energy := clampFloat(d.MotionEnergy, 0, 1)
	label := fmt.Sprintf("PCM SYNC  %03d%%", int(energy*100))
	if d.Paused {
		label = "SIGNAL HOLD  ·  PAUSED"
	}

	border := sDim.Render("╭" + strings.Repeat("─", inner) + "╮")
	footer := sDim.Render("╰" + strings.Repeat("─", inner) + "╯")
	return "  " + border + "\n" +
		"  " + reactorRow(inner, d, 0) + "\n" +
		"  " + reactorRow(inner, d, 1) + "\n" +
		"  " + reactorRow(inner, d, 2) + "\n" +
		"  " + sDim.Render("│") + centerStyled(sSub.Render(label), inner) + sDim.Render("│") + "\n" +
		"  " + footer
}

func reactorRow(width int, d ViewData, row int) string {
	cells := make([]string, width)
	for i := range cells {
		cells[i] = " "
	}

	energy := clampFloat(d.MotionEnergy, 0, 1)
	beat := clampFloat(d.MotionBeat, 0, 1)
	frame := int(d.MotionFrame)
	center := width / 2
	radius := 2 + int(beat*float64(min(width/5, 12)))

	// Animated data particles: changing only while PCM frames are received.
	particleCount := int(energy*8 + 0.5)
	for n := 0; n < particleCount; n++ {
		speed := 1 + n%3 + int(energy*3)
		position := (frame*speed + n*17 + row*11) % width
		if row == 1 && abs(position-center) < radius+2 {
			continue
		}
		if n%2 == 0 {
			cells[position] = sCyan.Render("·")
		} else {
			cells[position] = sBrand.Render("✦")
		}
	}

	// A three-row orbital core that expands only on a measured transient.
	switch row {
	case 0:
		put(cells, center-radius, sViolet.Render("╭"))
		put(cells, center+radius, sViolet.Render("╮"))
		for i := center - radius + 1; i < center+radius; i++ {
			put(cells, i, sViolet.Render("─"))
		}
	case 1:
		put(cells, center-radius-1, sBrand.Render("◌"))
		put(cells, center+radius+1, sBrand.Render("◌"))
		put(cells, center, sCyan.Render("◉"))
		if beat > 0.34 {
			put(cells, center-1, sText.Render("◆"))
			put(cells, center+1, sText.Render("◆"))
		}
	case 2:
		put(cells, center-radius, sViolet.Render("╰"))
		put(cells, center+radius, sViolet.Render("╯"))
		for i := center - radius + 1; i < center+radius; i++ {
			put(cells, i, sViolet.Render("─"))
		}
	}

	return sDim.Render("│") + strings.Join(cells, "") + sDim.Render("│")
}

func renderPlaylist(d ViewData) string {
	if len(d.PlaylistWindow) == 0 {
		return ""
	}
	maxWidth := max(d.Width-14, 15)
	var lines []string
	for _, entry := range d.PlaylistWindow {
		artistLimit := max(maxWidth/3, 10)
		artist := truncate(entry.Artist, artistLimit)
		title := truncate(entry.Title, maxWidth-artistLimit-3)
		if entry.IsCurrent {
			lines = append(lines, "  "+sBrand.Render("▸ ")+sCyan.Render(artist)+sDim.Render(" — ")+sText.Render(title))
		} else {
			lines = append(lines, "    "+sSub.Render(artist)+sDim.Render(" — ")+sSub.Render(title))
		}
	}
	return strings.Join(lines, "\n")
}

func renderWarnings(d ViewData) string {
	if len(d.SkippedMsgs) == 0 {
		return ""
	}
	var lines []string
	for _, warning := range d.SkippedMsgs {
		lines = append(lines, "  "+sWarn.Render(warning))
	}
	return strings.Join(lines, "\n")
}

func renderControls() string {
	controls := [][2]string{
		{"space", "pause"}, {"←/p", "prev"}, {"→/n", "next"},
		{"r", "shuffle"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(controls))
	for _, control := range controls {
		parts = append(parts, sKey.Render("["+control[0]+"]")+sSub.Render(" "+control[1]))
	}
	return "  " + strings.Join(parts, sDim.Render("  ·  "))
}

// centerScene positions the complete player in the middle of the terminal.
// The player itself never moves; only the ambient field around it responds to
// the music, travelling outwards from the screen's centre.
func centerScene(content string, d ViewData) string {
	terminalWidth := max(d.Width, 1)
	terminalHeight := max(d.Height, 1)
	frameWidth := min(sceneWidth(terminalWidth), terminalWidth)

	// Render the inner scene at its own width, not the terminal's full width.
	// This makes centring visible on wide screens.
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = padRendered(lines[i], frameWidth)
	}
	frameHeight := len(lines)
	left := max((terminalWidth-frameWidth)/2, 0)
	top := max((terminalHeight-frameHeight)/2, 0)

	ambient := ambientField(terminalWidth, terminalHeight, d)
	canvas := make([]string, terminalHeight)
	for row := range canvas {
		canvas[row] = strings.Join(ambient[row], "")
	}
	for row, line := range lines {
		target := top + row
		if target < 0 || target >= terminalHeight {
			continue
		}
		// The centred player is a static mask over the radial field.
		canvas[target] = strings.Join(ambient[target][:left], "") + line +
			strings.Join(ambient[target][left+frameWidth:], "")
	}
	return strings.Join(canvas, "\n")
}

// ambientField creates a true two-dimensional radial burst. Every particle
// gets an angle and an age, therefore both x and y originate from the centre;
// the core panel simply masks the middle of the field.
func ambientField(width, height int, d ViewData) [][]string {
	cells := make([][]string, height)
	for row := range cells {
		cells[row] = make([]string, width)
		for col := range cells[row] {
			cells[row][col] = " "
		}
	}
	if width == 0 || height == 0 {
		return cells
	}
	energy := clampFloat(d.MotionEnergy, 0, 1)
	beat := clampFloat(d.MotionBeat, 0, 1)
	count := int(energy*16 + beat*18)
	if d.PulseAge < 10 {
		count += int(d.PulseStrength * 8)
	}
	maxRadius := math.Hypot(float64(width)/2, float64(height)/2)
	frame := int(d.MotionFrame)
	for particle := 0; particle < count; particle++ {
		angle := float64(particle)*2.399963229728653 + 0.42 // golden-angle spread
		life := 14 + particle%11
		age := (frame*(1+particle%3) + particle*5) % life
		radius := float64(age) / float64(life) * maxRadius
		placeRadial(cells, width, height, angle, radius, ambientGlyph(particle))
		// A short fading tail makes each ray feel like it is escaping the core.
		if age > 1 {
			placeRadial(cells, width, height, angle, radius-2.2, sDim.Render("·"))
		}
	}

	// A measured transient emits a circular shockwave that grows outward for
	// roughly 700 ms. Unlike a looping decoration, it starts only on a beat.
	if d.PulseStrength > 0.16 && d.PulseAge < 14 {
		radius := float64(d.PulseAge) * (1.4 + d.PulseStrength)
		drawShockwave(cells, width, height, radius)
		if d.PulseStrength > 0.55 && d.PulseAge < 9 {
			drawShockwave(cells, width, height, radius*0.58)
		}
	}
	return cells
}

func placeRadial(cells [][]string, width, height int, angle, radius float64, glyph string) {
	centreX, centreY := float64(width-1)/2, float64(height-1)/2
	// Terminal cells are taller than they are wide; compensate to keep circles
	// circular to the eye while still moving on both axes.
	x := int(math.Round(centreX + math.Cos(angle)*radius))
	y := int(math.Round(centreY + math.Sin(angle)*radius*0.48))
	if y >= 0 && y < height && x >= 0 && x < width {
		cells[y][x] = glyph
	}
}

func drawShockwave(cells [][]string, width, height int, radius float64) {
	if radius < 1 {
		return
	}
	centreX, centreY := float64(width-1)/2, float64(height-1)/2
	for step := 0; step < 48; step++ {
		angle := float64(step) * 2 * math.Pi / 48
		x := int(math.Round(centreX + math.Cos(angle)*radius))
		y := int(math.Round(centreY + math.Sin(angle)*radius*0.48))
		if y >= 0 && y < height && x >= 0 && x < width {
			if step%3 == 0 {
				cells[y][x] = sBrand.Render("✦")
			} else {
				cells[y][x] = sViolet.Render("·")
			}
		}
	}
}

func ambientGlyph(particle int) string {
	if particle%3 == 0 {
		return sCyan.Render("·")
	}
	if particle%3 == 1 {
		return sBrand.Render("✦")
	}
	return sViolet.Render("╱")
}

func padRendered(value string, width int) string {
	return value + strings.Repeat(" ", max(width-lipgloss.Width(value), 0))
}

func sceneWidth(terminalWidth int) int {
	return min(max(terminalWidth-6, 28), 92)
}

func centerStyled(value string, width int) string {
	padding := max((width-lipgloss.Width(value))/2, 0)
	return strings.Repeat(" ", padding) + value + strings.Repeat(" ", max(width-lipgloss.Width(value)-padding, 0))
}

func put(cells []string, index int, value string) {
	if index >= 0 && index < len(cells) {
		cells[index] = value
	}
}

func truncate(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit-1]) + "…"
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

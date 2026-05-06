// Package visualizer generates animated frequency-bar visuals that mimic cava.
// Bars are driven by a physics-inspired model: fast attack, slow decay,
// a bass-dominant spectral envelope, and a periodic beat pulse.
package visualizer

import (
	"math"
	"math/rand"
)

// Viz holds the state for one set of animated bars.
type Viz struct {
	count   int
	target  []float64 // per-bar height goal  [0..1]
	current []float64 // smoothed display height [0..1]
	phase   []float64 // oscillation phase per bar
	speed   []float64 // oscillation speed per bar
	energy  float64   // global energy [0..1]
	beat    float64   // beat impulse [0..1]
	tick    int
}

// New creates a Viz with the given number of bars.
func New(count int) *Viz {
	v := &Viz{count: count}
	v.init()
	return v
}

func (v *Viz) init() {
	v.target = make([]float64, v.count)
	v.current = make([]float64, v.count)
	v.phase = make([]float64, v.count)
	v.speed = make([]float64, v.count)

	for i := range v.phase {
		v.phase[i] = rand.Float64() * math.Pi * 2
		v.speed[i] = 0.05 + rand.Float64()*0.15
	}
	v.energy = 0.65
}

// Resize adjusts the bar count, resetting all state.
func (v *Viz) Resize(count int) {
	v.count = count
	v.init()
}

// Reset zeroes heights (call when starting a new track).
func (v *Viz) Reset() {
	for i := range v.current {
		v.current[i] = 0
		v.target[i] = 0
	}
	v.beat = 0
	v.energy = 0.65
	v.tick = 0
}

// Tick advances the animation by one frame (~50 ms).
func (v *Viz) Tick() {
	v.tick++

	// Slowly drift the global energy up and down.
	v.energy += (rand.Float64()*0.3 - 0.1) * 0.08
	v.energy = clamp(v.energy, 0.30, 1.0)

	// Beat impulse at ~130 BPM (≈ 1.15 s → 23 ticks at 50 ms).
	const beatInterval = 23
	phase := v.tick % beatInterval
	if phase < 4 {
		v.beat = 1.0 - float64(phase)/4.0
	} else {
		v.beat *= 0.80
	}

	for i := 0; i < v.count; i++ {
		pos := float64(i) / float64(max(v.count-1, 1))

		// Spectral envelope: dominant bass, mid presence, airy highs.
		spectral := spectralWeight(pos)

		// Organic oscillation per bar.
		v.phase[i] += v.speed[i]
		osc := (math.Sin(v.phase[i]) + 1.0) / 2.0

		// Beat energy boost concentrated on the bass end.
		beatBoost := 0.0
		if pos < 0.3 {
			beatBoost = v.beat * (1.0 - pos/0.3) * 0.55
		}

		t := (spectral*0.55 + osc*0.45) * v.energy
		t = clamp(t+beatBoost, 0.02, 1.0)
		v.target[i] = t

		// Attack fast, decay slow — the signature cava feel.
		if t > v.current[i] {
			v.current[i] += (t - v.current[i]) * 0.60
		} else {
			v.current[i] += (t - v.current[i]) * 0.12
		}
	}
}

// Heights returns a snapshot of the current bar heights [0..1].
func (v *Viz) Heights() []float64 {
	out := make([]float64, v.count)
	copy(out, v.current)
	return out
}

// spectralWeight returns a weight [0..1] for a normalised frequency position.
// Shape: strong bass (0), mid bump (0.4), soft highs (0.8).
func spectralWeight(pos float64) float64 {
	bass := math.Exp(-pos*4.5) * 0.85
	mid := math.Exp(-math.Pow((pos-0.40)*4.0, 2)) * 0.50
	high := math.Exp(-math.Pow((pos-0.80)*6.0, 2)) * 0.22
	return clamp(bass+mid+high, 0, 1)
}

func clamp(v, lo, hi float64) float64 {
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

// Package visualizer turns real PCM samples into a compact motion signal for
// the terminal's cyberpunk animation. It intentionally contains no fake tempo
// or synthetic frequency bars.
package visualizer

import (
	"math"
	"sync"
)

// Snapshot is an immutable rendering frame. All values are normalised [0..1].
type Snapshot struct {
	Energy        float64
	Beat          float64
	Frame         uint64
	PulseAge      uint64
	PulseStrength float64
}

// Viz tracks loudness and transients from the audio currently playing.
type Viz struct {
	mu       sync.RWMutex
	energy   float64
	baseline float64
	beat     float64
	frame    uint64
	pulseAt  uint64
	pulse    float64
}

func New(_ int) *Viz        { return &Viz{} }
func (v *Viz) Resize(_ int) {}

func (v *Viz) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.energy, v.baseline, v.beat, v.frame, v.pulseAt, v.pulse = 0, 0, 0, 0, 0, 0
}

// Observe receives PCM produced by the player. RMS provides loudness; the
// amount by which it rises above its rolling average is the beat impulse.
func (v *Viz) Observe(samples [][2]float64) {
	if len(samples) == 0 {
		return
	}
	var sum float64
	for _, sample := range samples {
		mono := (sample[0] + sample[1]) * 0.5
		sum += mono * mono
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	energy := clamp(rms*2.6, 0, 1)

	v.mu.Lock()
	defer v.mu.Unlock()
	if energy > v.energy {
		v.energy += (energy - v.energy) * 0.72
	} else {
		v.energy += (energy - v.energy) * 0.20
	}
	v.baseline += (energy - v.baseline) * 0.055
	impulse := clamp((energy-v.baseline)*8, 0, 1)
	if impulse > v.beat {
		v.beat = impulse
	}
	// Remember the onset so the UI can send a visible shockwave outward after
	// the short beat value itself has already decayed.
	if impulse > 0.16 && (v.frame-v.pulseAt > 2 || impulse > v.pulse+0.15) {
		v.pulseAt = v.frame
		v.pulse = impulse
	}
}

// Tick advances visual time and lets the light trail decay between samples.
func (v *Viz) Tick() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frame++
	v.beat *= 0.79
	v.energy *= 0.985
}

func (v *Viz) Snapshot() Snapshot {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return Snapshot{
		Energy:        v.energy,
		Beat:          v.beat,
		Frame:         v.frame,
		PulseAge:      v.frame - v.pulseAt,
		PulseStrength: v.pulse,
	}
}

func clamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

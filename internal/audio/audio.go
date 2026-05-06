// Package audio handles MP3 decoding and playback via gopxl/beep.
// It resamples all audio to a fixed 44100 Hz output rate and gracefully
// skips files that cannot be decoded.
package audio

import (
	"fmt"
	"os"
	"sync"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

const (
	SampleRate = beep.SampleRate(44100)
	BufferSize = 4096
)

// DoneFunc is called (from the speaker goroutine) when a track finishes naturally.
type DoneFunc func()

// Engine manages speaker initialisation and playback of a single track at a time.
type Engine struct {
	mu       sync.Mutex
	ctrl     *beep.Ctrl
	streamer beep.StreamCloser
	paused   bool
	ready    bool
}

// New initialises the audio engine. Call once at startup.
func New() (*Engine, error) {
	if err := speaker.Init(SampleRate, BufferSize); err != nil {
		return nil, fmt.Errorf("speaker init: %w", err)
	}
	return &Engine{ready: true}, nil
}

// Play opens path and starts playback. When the track ends naturally, onDone
// is called. Returns a non-nil error if the file cannot be opened or decoded —
// the caller should skip to the next track in that case.
func (e *Engine) Play(path string, onDone DoneFunc) error {
	e.Stop()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("mp3 decode: %w", err)
	}

	e.mu.Lock()
	e.streamer = streamer
	e.paused = false

	var resampled beep.Streamer = streamer
	if format.SampleRate != SampleRate {
		resampled = beep.Resample(4, format.SampleRate, SampleRate, streamer)
	}

	ctrl := &beep.Ctrl{Streamer: resampled}
	e.ctrl = ctrl
	e.mu.Unlock()

	speaker.Play(beep.Seq(ctrl, beep.Callback(func() {
		if onDone != nil {
			onDone()
		}
	})))

	return nil
}

// TogglePause flips the paused state and returns the new paused value.
func (e *Engine) TogglePause() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctrl == nil {
		return false
	}
	speaker.Lock()
	e.ctrl.Paused = !e.ctrl.Paused
	e.paused = e.ctrl.Paused
	speaker.Unlock()
	return e.paused
}

// Paused returns whether playback is currently paused.
func (e *Engine) Paused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// Stop halts playback and closes the current streamer.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	speaker.Lock()
	if e.ctrl != nil {
		e.ctrl.Paused = true
	}
	speaker.Unlock()

	speaker.Clear()

	if e.streamer != nil {
		_ = e.streamer.Close()
		e.streamer = nil
	}
	e.ctrl = nil
	e.paused = false
}

// Close shuts down the engine permanently.
func (e *Engine) Close() {
	e.Stop()
}

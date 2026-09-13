// Package audio owns decoding, playback and the small audio tap used by the UI.
//
// ffmpeg is used as the primary decoder because it is tolerant of damaged tags
// and supports broad format coverage. MP3 has a lightweight in-process fallback
// when ffmpeg is unavailable. Sound still goes through beep.
package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

const (
	SampleRate = beep.SampleRate(44100)
	BufferSize = 4096
)

// SamplesFunc receives the real samples currently being sent to the speaker.
// It is called from the audio goroutine and must return quickly.
type SamplesFunc func([][2]float64)

// Engine manages one track at a time.
type Engine struct {
	mu sync.Mutex

	ctrl     *beep.Ctrl
	streamer beep.StreamCloser
	paused   bool

	generation    uint64
	completed     bool
	completionErr error
}

// New initialises the audio engine. Call once at startup.
func New() (*Engine, error) {
	if err := speaker.Init(SampleRate, BufferSize); err != nil {
		return nil, fmt.Errorf("speaker init: %w", err)
	}
	return &Engine{}, nil
}

// Play opens path, starts it and forwards its actual PCM samples to onSamples.
// Files that cannot be decoded are reported to the caller, which can skip them.
func (e *Engine) Play(path string, onSamples SamplesFunc) error {
	e.Stop()

	streamer, format, err := decode(path)
	if err != nil {
		return err
	}

	var output beep.Streamer = streamer
	if format.SampleRate != SampleRate {
		output = beep.Resample(4, format.SampleRate, SampleRate, output)
	}
	output = &sampleTap{source: output, observe: onSamples}

	e.mu.Lock()
	e.generation++
	generation := e.generation
	e.streamer = streamer
	e.ctrl = &beep.Ctrl{Streamer: output}
	e.paused = false
	e.completed = false
	e.completionErr = nil
	ctrl := e.ctrl
	e.mu.Unlock()

	speaker.Play(beep.Seq(ctrl, beep.Callback(func() {
		e.finish(generation, streamer.Err())
	})))
	return nil
}

// ConsumeCompletion reports a natural end (or a decoding failure during
// playback) exactly once. It is safe to call from Bubble Tea's update loop.
func (e *Engine) ConsumeCompletion() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.completed {
		return false, nil
	}
	e.completed = false
	return true, e.completionErr
}

func (e *Engine) finish(generation uint64, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if generation != e.generation {
		return // completion from a track that was manually replaced
	}
	e.completed = true
	e.completionErr = err
}

// TogglePause flips the paused state. The second return value says whether a
// track was active and therefore actually changed state.
func (e *Engine) TogglePause() (bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctrl == nil {
		return false, false
	}
	speaker.Lock()
	e.ctrl.Paused = !e.ctrl.Paused
	speaker.Unlock()
	e.paused = e.ctrl.Paused
	return e.paused, true
}

// Stop halts and releases the current track.
func (e *Engine) Stop() {
	e.mu.Lock()
	e.generation++
	ctrl := e.ctrl
	streamer := e.streamer
	e.ctrl = nil
	e.streamer = nil
	e.paused = false
	e.completed = false
	e.completionErr = nil
	e.mu.Unlock()

	if ctrl != nil {
		speaker.Lock()
		ctrl.Paused = true
		speaker.Unlock()
	}
	speaker.Clear()
	if streamer != nil {
		_ = streamer.Close()
	}
}

// Close releases the currently playing stream.
func (e *Engine) Close() { e.Stop() }

func decode(path string) (beep.StreamCloser, beep.Format, error) {
	// ffmpeg copes with malformed ID3/header data and covers FLAC, Ogg/Opus,
	// AAC/M4A, WAV, AIFF and many more containers.
	streamer, err := newFFmpegStreamer(path)
	if err == nil {
		return streamer, beep.Format{SampleRate: SampleRate, NumChannels: 2, Precision: 4}, nil
	}

	// Keep plain MP3 usable on minimal systems where ffmpeg is not installed.
	if strings.EqualFold(filepath.Ext(path), ".mp3") {
		file, err := os.Open(path)
		if err == nil {
			streamer, format, decodeErr := mp3.Decode(file)
			if decodeErr == nil {
				return streamer, format, nil
			}
			_ = file.Close()
		}
	}

	return nil, beep.Format{}, fmt.Errorf("decode %q: %w", filepath.Base(path), err)
}

// sampleTap observes decoded PCM without altering it.
type sampleTap struct {
	source  beep.Streamer
	observe SamplesFunc
}

func (t *sampleTap) Stream(samples [][2]float64) (int, bool) {
	n, ok := t.source.Stream(samples)
	if n > 0 && t.observe != nil {
		t.observe(samples[:n])
	}
	return n, ok
}

func (t *sampleTap) Err() error { return t.source.Err() }

// ffmpegStreamer exposes ffmpeg's f32 PCM output as a beep.StreamCloser.
type ffmpegStreamer struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr bytes.Buffer

	mu     sync.Mutex
	stream sync.Mutex
	buf    []byte
	err    error
	waited sync.Once
	closed sync.Once
}

func newFFmpegStreamer(path string) (*ffmpegStreamer, error) {
	cmd := exec.Command("ffmpeg", "-v", "error", "-nostdin", "-i", path,
		"-vn", "-sn", "-dn", "-f", "f32le", "-ac", "2", "-ar", "44100", "pipe:1")
	s := &ffmpegStreamer{cmd: cmd}
	cmd.Stderr = &s.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	s.stdout = stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg is required for this format: %w", err)
	}
	return s, nil
}

func (s *ffmpegStreamer) Stream(samples [][2]float64) (int, bool) {
	s.stream.Lock()
	defer s.stream.Unlock()

	want := len(samples) * 8 // two float32 channels per frame
	if cap(s.buf) < want {
		s.buf = make([]byte, want)
	} else {
		s.buf = s.buf[:want]
	}
	n, readErr := io.ReadFull(s.stdout, s.buf)
	frames := n / 8
	for i := 0; i < frames; i++ {
		offset := i * 8
		samples[i][0] = float64(math.Float32frombits(binary.LittleEndian.Uint32(s.buf[offset:])))
		samples[i][1] = float64(math.Float32frombits(binary.LittleEndian.Uint32(s.buf[offset+4:])))
	}
	if readErr != nil {
		s.wait()
		return frames, false
	}
	return frames, true
}

func (s *ffmpegStreamer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *ffmpegStreamer) Close() error {
	s.closed.Do(func() {
		_ = s.stdout.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		s.wait()
	})
	return nil
}

func (s *ffmpegStreamer) wait() {
	s.waited.Do(func() {
		err := s.cmd.Wait()
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return
		}
		// A killed process is an expected result of Stop, so it is not surfaced.
		if s.cmd.ProcessState != nil && !s.cmd.ProcessState.Success() && s.stderr.Len() > 0 {
			s.mu.Lock()
			s.err = fmt.Errorf("ffmpeg: %s", strings.TrimSpace(s.stderr.String()))
			s.mu.Unlock()
		}
	})
}

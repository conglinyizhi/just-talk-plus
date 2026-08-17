// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// AutoGain keeps speech in a useful range before it is sent to ASR. It gates
// adjustment on the unamplified input, so silence does not continually raise
// gain, while the output peak determines how quickly the gain is corrected.
type AutoGain struct {
	enabled bool
	gain    atomic.Int32

	targetPeak    float64
	minPeak       float64
	maxPeak       float64
	clipLimit     float64
	voicePeakMin  float64
	voiceRMSMin   float64
	maxRiseFactor float64
	minGain       int32
	maxGain       int32

	mu sync.Mutex
}

// NewAutoGain constructs an AGC controller. The target leaves headroom for
// louder syllables while lifting consistently weak microphones quickly.
func NewAutoGain(enabled bool, initial int) *AutoGain {
	a := &AutoGain{
		enabled:       enabled,
		targetPeak:    0.60,
		minPeak:       0.32,
		maxPeak:       0.82,
		clipLimit:     0.96,
		voicePeakMin:  0.004,
		voiceRMSMin:   0.0007,
		maxRiseFactor: 1.7,
		minGain:       int32(MinGain),
		maxGain:       int32(MaxGain),
	}
	a.gain.Store(int32(clampGain(initial)))
	return a
}

func (a *AutoGain) Enabled() bool      { return a.enabled }
func (a *AutoGain) SetEnabled(on bool) { a.enabled = on }

// Gain returns the current multiplier.
func (a *AutoGain) Gain() int { return int(a.gain.Load()) }

// SetGain changes the manual baseline. Further automatic adjustment starts
// from this value.
func (a *AutoGain) SetGain(g int) { a.gain.Store(int32(clampGain(g))) }

// Process observes one PCM block. rawPeak/rawRMS are measured before gain,
// outputPeak after gain, and limited reports that the soft limiter engaged.
// The time parameter is retained for callers that sample at explicit times.
func (a *AutoGain) Process(rawPeak, rawRMS, outputPeak float64, limited bool, _ time.Time) bool {
	if !a.enabled {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// Do not chase the noise floor. A weak microphone still clears both limits
	// during speech (for example peak 0.01 / RMS 0.002), while quiet room tone
	// normally does not.
	if rawPeak < a.voicePeakMin || rawRMS < a.voiceRMSMin {
		return false
	}

	current := a.gain.Load()
	next := current
	switch {
	case limited || outputPeak >= a.clipLimit:
		// Limiting means this gain is already unsafe. Drop decisively so the next
		// block has usable headroom instead of waiting for repeated hard clips.
		next = int32(math.Floor(float64(current) * 0.65))
	case outputPeak > a.maxPeak:
		ratio := a.targetPeak / outputPeak
		if ratio < 0.65 {
			ratio = 0.65
		}
		next = int32(math.Floor(float64(current) * ratio))
	case outputPeak < a.minPeak:
		denominator := math.Max(outputPeak, 0.01)
		ratio := a.targetPeak / denominator
		if ratio > a.maxRiseFactor {
			ratio = a.maxRiseFactor
		}
		next = int32(math.Ceil(float64(current) * ratio))
	}

	if next < a.minGain {
		next = a.minGain
	}
	if next > a.maxGain {
		next = a.maxGain
	}
	if next == current {
		return false
	}
	a.gain.Store(next)
	return true
}

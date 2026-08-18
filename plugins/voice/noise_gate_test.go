// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import "testing"

func TestCalibratedNoiseThreshold(t *testing.T) {
	if got := calibratedNoiseThreshold([]float64{0.01, 0.012, 0.011, 0.01, 0.013}); got != 0.03 {
		t.Fatalf("calibrated threshold = %.4f, want %.4f", got, 0.03)
	}
	if got := calibratedNoiseThreshold(nil); got != defaultNoiseThreshold {
		t.Fatalf("empty calibration = %.4f, want %.4f", got, defaultNoiseThreshold)
	}
}

func TestNoiseCalibrationSnapshot(t *testing.T) {
	micTest.mu.Lock()
	originalVersion := micTest.noiseCalibrationVersion
	originalThreshold := micTest.level.NoiseThreshold
	micTest.noiseCalibrationVersion = 42
	micTest.level.NoiseThreshold = 0.0234
	micTest.mu.Unlock()
	t.Cleanup(func() {
		micTest.mu.Lock()
		micTest.noiseCalibrationVersion = originalVersion
		micTest.level.NoiseThreshold = originalThreshold
		micTest.mu.Unlock()
	})

	version, threshold := NoiseCalibrationSnapshot()
	if version != 42 || threshold != 0.0234 {
		t.Fatalf("snapshot = (%d, %.4f), want (42, 0.0234)", version, threshold)
	}
}

func TestVoiceAboveNoise(t *testing.T) {
	if voiceAboveNoise(0.01, 0.02, 0.015) {
		t.Fatal("quiet input should remain below noise threshold")
	}
	if !voiceAboveNoise(0.016, 0.01, 0.015) {
		t.Fatal("RMS above threshold should pass")
	}
	if !voiceAboveNoise(0.01, 0.05, 0.015) {
		t.Fatal("peak above threshold should pass")
	}
}

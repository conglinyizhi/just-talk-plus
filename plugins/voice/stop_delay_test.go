// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import "testing"

func TestNormalizeStopDelayMs(t *testing.T) {
	for _, test := range []struct {
		name string
		in   int
		want int
	}{
		{name: "missing", in: 0, want: defaultStopDelayMs},
		{name: "negative", in: -1, want: defaultStopDelayMs},
		{name: "configured", in: 6000, want: 6000},
		{name: "bounded", in: maxStopDelayMs + 1, want: maxStopDelayMs},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeStopDelayMs(test.in); got != test.want {
				t.Fatalf("normalizeStopDelayMs(%d) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

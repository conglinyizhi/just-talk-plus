//go:build linux

package voice

import "testing"

func TestParseALSAHardwareCards(t *testing.T) {
	out := `**** List of CAPTURE Hardware Devices ****
card 0: NVidia [HDA NVidia], device 3: HDMI 0 Digital [HDMI 0 Digital]
  Subdevices: 1/1
card 3: Audio [AB13X USB Audio], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
`
	devices := parseALSAHardwareCards(out)
	if len(devices) != 2 {
		t.Fatalf("devices = %v, want 2", devices)
	}
	if devices[0] != "plughw:0" || devices[1] != "plughw:3" {
		t.Fatalf("devices = %v", devices)
	}
}

func TestParseALSAHardwareCardsEmpty(t *testing.T) {
	if devices := parseALSAHardwareCards("arecord: device_list:279: no soundcards found...\n"); len(devices) != 0 {
		t.Fatalf("devices = %v, want none", devices)
	}
}

func TestIsPulseSource(t *testing.T) {
	for _, tt := range []struct {
		dev  string
		want bool
	}{
		{"alsa_input.usb-AB13X.mono-fallback", true},
		{"input.pci-0000_0b_00.6.analog-stereo", true},
		{"plughw:3", false},
		{"hw:0", false},
		{"pipewire", false},
	} {
		if got := isPulseSource(tt.dev); got != tt.want {
			t.Fatalf("isPulseSource(%q) = %v, want %v", tt.dev, got, tt.want)
		}
	}
}

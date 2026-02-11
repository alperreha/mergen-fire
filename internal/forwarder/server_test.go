package forwarder

import (
	"testing"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestIsPlainFirstVMSSHListener(t *testing.T) {
	cases := []struct {
		name     string
		listener Listener
		want     bool
	}{
		{
			name:     "plain ssh test listener",
			listener: Listener{Addr: ":2022", GuestPort: 22},
			want:     true,
		},
		{
			name:     "standard tls https listener",
			listener: Listener{Addr: ":443", GuestPort: 443},
			want:     false,
		},
		{
			name:     "ssh guest on different host port",
			listener: Listener{Addr: ":2222", GuestPort: 22},
			want:     false,
		},
	}

	for _, tc := range cases {
		got := isPlainFirstVMSSHListener(tc.listener)
		if got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestRequiresTLSCertificate(t *testing.T) {
	plainOnly := []Listener{
		{Addr: ":2022", GuestPort: 22},
	}
	if requiresTLSCertificate(plainOnly) {
		t.Fatalf("plain-only listener set should not require tls certificate")
	}

	mixed := []Listener{
		{Addr: ":2022", GuestPort: 22},
		{Addr: ":443", GuestPort: 443},
	}
	if !requiresTLSCertificate(mixed) {
		t.Fatalf("mixed listener set should require tls certificate")
	}
}

func TestResolveTargetGuestPort(t *testing.T) {
	cases := []struct {
		name     string
		listener Listener
		meta     model.VMMetadata
		want     int
	}{
		{
			name:     "https listener uses vm http port",
			listener: Listener{Addr: ":443", GuestPort: 443},
			meta:     model.VMMetadata{HTTPPort: 80},
			want:     80,
		},
		{
			name:     "https listener falls back to listener guest port",
			listener: Listener{Addr: ":443", GuestPort: 443},
			meta:     model.VMMetadata{},
			want:     443,
		},
		{
			name:     "non-https listener keeps static guest port",
			listener: Listener{Addr: ":5432", GuestPort: 5432},
			meta:     model.VMMetadata{HTTPPort: 80},
			want:     5432,
		},
	}

	for _, tc := range cases {
		got := resolveTargetGuestPort(tc.listener, tc.meta)
		if got != tc.want {
			t.Fatalf("%s: expected %d, got %d", tc.name, tc.want, got)
		}
	}
}

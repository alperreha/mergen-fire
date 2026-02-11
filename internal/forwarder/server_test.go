package forwarder

import (
	"testing"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestTargetHTTPPort(t *testing.T) {
	cases := []struct {
		name     string
		meta     model.VMMetadata
		wantPort int
		wantErr  bool
	}{
		{
			name:     "valid http port",
			meta:     model.VMMetadata{HTTPPort: 80},
			wantPort: 80,
		},
		{
			name:    "missing http port",
			meta:    model.VMMetadata{},
			wantErr: true,
		},
		{
			name:    "out of range http port",
			meta:    model.VMMetadata{HTTPPort: 70000},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		got, err := targetHTTPPort(tc.meta)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.wantPort {
			t.Fatalf("%s: expected port %d, got %d", tc.name, tc.wantPort, got)
		}
	}
}

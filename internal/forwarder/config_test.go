package forwarder

import "testing"

func TestNormalizeListenAddr(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "default style", raw: ":443", want: ":443"},
		{name: "port only", raw: "8443", want: ":8443"},
		{name: "host and port", raw: "127.0.0.1:443", want: "127.0.0.1:443"},
		{name: "invalid", raw: "abc", wantErr: true},
	}

	for _, tc := range cases {
		got, err := normalizeListenAddr(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.want, got)
		}
	}
}

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

func TestFromEnv_UsesSingleDomain(t *testing.T) {
	t.Setenv("FWD_DOMAIN", "vm.example.com")
	t.Setenv("FWD_TLS_CERT_FILE", "")
	t.Setenv("FWD_TLS_KEY_FILE", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() returned error: %v", err)
	}

	if cfg.Domain != "vm.example.com" {
		t.Fatalf("Domain mismatch: got %q want %q", cfg.Domain, "vm.example.com")
	}
	if cfg.CertFile != "/var/lib/mergen/certs/wildcard.vm.example.com.crt" {
		t.Fatalf("CertFile mismatch: got %q", cfg.CertFile)
	}
	if cfg.KeyFile != "/var/lib/mergen/certs/wildcard.vm.example.com.key" {
		t.Fatalf("KeyFile mismatch: got %q", cfg.KeyFile)
	}
}

func TestFromEnv_InvalidDomain(t *testing.T) {
	t.Setenv("FWD_DOMAIN", ".")
	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for invalid FWD_DOMAIN, got nil")
	}
}

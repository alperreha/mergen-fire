package converter

import "testing"

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "nginx:alpine", want: "nginx-alpine"},
		{in: "ghcr.io/org/app:1.2.3", want: "ghcr.io-org-app-1.2.3"},
		{in: "@@@", want: "image-rootfs"},
	}

	for _, tc := range cases {
		got := sanitizeName(tc.in)
		if got != tc.want {
			t.Fatalf("sanitizeName(%q) => %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestComposeStartCommand(t *testing.T) {
	got := composeStartCommand([]string{"python"}, []string{"app.py"})
	if len(got) != 2 || got[0] != "python" || got[1] != "app.py" {
		t.Fatalf("unexpected start command: %#v", got)
	}

	fallback := composeStartCommand(nil, nil)
	if len(fallback) != 1 || fallback[0] != "/bin/sh" {
		t.Fatalf("unexpected fallback command: %#v", fallback)
	}
}

func TestInferHTTPPort(t *testing.T) {
	cases := []struct {
		name  string
		ports map[string]struct{}
		want  int
	}{
		{
			name: "prefers common http port",
			ports: map[string]struct{}{
				"5432/tcp": {},
				"3000/tcp": {},
			},
			want: 3000,
		},
		{
			name: "falls back to first tcp port",
			ports: map[string]struct{}{
				"7001/tcp": {},
				"9000/tcp": {},
			},
			want: 7001,
		},
		{
			name:  "returns zero when no ports",
			ports: map[string]struct{}{},
			want:  0,
		},
	}

	for _, tc := range cases {
		got := inferHTTPPort(tc.ports)
		if got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

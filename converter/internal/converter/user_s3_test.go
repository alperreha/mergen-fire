package converter

import "testing"

func TestPayloadObjectKey(t *testing.T) {
	t.Parallel()

	profile := RegistryProfile{
		Prefix:   "users",
		Username: "alice",
	}

	got := payloadObjectKey(profile, "ghcr.io/acme/app:1.2.3")
	want := "users/alice/payload/ghcr.io%2Facme%2Fapp:1.2.3/payload-rootfs.ext4"
	if got != want {
		t.Fatalf("payload object key mismatch: got %q want %q", got, want)
	}
}

func TestPayloadObjectKeyEscapesUsername(t *testing.T) {
	t.Parallel()

	profile := RegistryProfile{
		Prefix:   "users",
		Username: "team/alice",
	}

	got := payloadObjectKey(profile, "nginx:alpine")
	want := "users/team%2Falice/payload/nginx:alpine/payload-rootfs.ext4"
	if got != want {
		t.Fatalf("payload object key mismatch: got %q want %q", got, want)
	}
}

package images

import "testing"

func TestNormalizeBaseRefDefaults(t *testing.T) {
	ref, err := NormalizeBaseRef("linux-amd64", "", "")
	if err != nil {
		t.Fatalf("normalize ref: %v", err)
	}

	if ref.Platform != "linux-amd64" {
		t.Fatalf("platform mismatch: got %q", ref.Platform)
	}
	if ref.Flavor != defaultBaseFlavor {
		t.Fatalf("flavor mismatch: got %q want %q", ref.Flavor, defaultBaseFlavor)
	}
	if ref.Version != defaultBaseVersion {
		t.Fatalf("version mismatch: got %q want %q", ref.Version, defaultBaseVersion)
	}
}

func TestNormalizeBaseRefRejectsEmptyPlatform(t *testing.T) {
	if _, err := NormalizeBaseRef("", "buildroot", "v0.0.1"); err == nil {
		t.Fatal("expected empty platform to fail")
	}
}

func TestNormalizeBaseRefSanitizesParts(t *testing.T) {
	ref, err := NormalizeBaseRef(" Linux/AMD64 ", "Buildroot!", " V0.0.1 ")
	if err != nil {
		t.Fatalf("normalize ref: %v", err)
	}

	if ref.Platform != "linuxamd64" {
		t.Fatalf("platform mismatch: got %q", ref.Platform)
	}
	if ref.Flavor != "buildroot" {
		t.Fatalf("flavor mismatch: got %q", ref.Flavor)
	}
	if ref.Version != "v0.0.1" {
		t.Fatalf("version mismatch: got %q", ref.Version)
	}
}

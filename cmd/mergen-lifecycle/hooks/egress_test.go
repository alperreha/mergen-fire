package hooks

import "testing"

func TestEgressVethNames(t *testing.T) {
	root, ns := EgressVethNames("6f008233-68f7-47b8-b2d1-6a9f0632b30b")

	if root != "mgnh6f008233" {
		t.Fatalf("unexpected root veth name: %s", root)
	}
	if ns != "mgnn6f008233" {
		t.Fatalf("unexpected namespace veth name: %s", ns)
	}
	if len(root) > 15 || len(ns) > 15 {
		t.Fatalf("veth names must fit linux interface limits: %q %q", root, ns)
	}
}

func TestDeriveEgressPeerCIDRs(t *testing.T) {
	rootCIDR, nsCIDR, rootIP, nsIP, err := DeriveEgressPeerCIDRs("172.30.0.2")
	if err != nil {
		t.Fatalf("derive egress cidrs: %v", err)
	}

	if rootCIDR != "100.64.0.9/30" {
		t.Fatalf("unexpected root cidr: %s", rootCIDR)
	}
	if nsCIDR != "100.64.0.10/30" {
		t.Fatalf("unexpected namespace cidr: %s", nsCIDR)
	}
	if rootIP != "100.64.0.9" {
		t.Fatalf("unexpected root ip: %s", rootIP)
	}
	if nsIP != "100.64.0.10" {
		t.Fatalf("unexpected namespace ip: %s", nsIP)
	}
}

func TestBuildEgressConfigRejectsInvalidGuestIP(t *testing.T) {
	if _, err := BuildEgressConfig("vm-1", "not-an-ip"); err == nil {
		t.Fatal("expected invalid guest ip to fail")
	}
}

func TestResolveHostUplinkOverrideWins(t *testing.T) {
	got, err := ResolveHostUplink("ens18")
	if err != nil {
		t.Fatalf("resolve host uplink with override: %v", err)
	}
	if got != "ens18" {
		t.Fatalf("unexpected uplink override result: %s", got)
	}
}

func TestIsUsableUplinkName(t *testing.T) {
	if !isUsableUplinkName("ens3") {
		t.Fatal("expected ens3 to be accepted as uplink")
	}
	for _, candidate := range []string{"lo", "tap-test", "mgnh1234", "mgnn1234", "vethabcd", "docker0", "cni0"} {
		if isUsableUplinkName(candidate) {
			t.Fatalf("expected %s to be rejected as uplink", candidate)
		}
	}
}

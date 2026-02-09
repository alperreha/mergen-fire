package network

import (
	"testing"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestAllocator_Allocate(t *testing.T) {
	a := NewAllocator(20000, 20010, "172.30.0.0/24")

	existing := []model.VMMetadata{
		{
			GuestIP: "172.30.0.2",
			Ports: []model.PortBinding{
				{Host: 20000, Guest: 8080, Protocol: "tcp"},
			},
		},
	}
	requests := []model.PortBindingRequest{
		{Guest: 80, Host: 0},
		{Guest: 443, Host: 20005},
	}

	ip, ports, err := a.Allocate(existing, requests)
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if ip != "172.30.0.3" {
		t.Fatalf("unexpected guest ip: %s", ip)
	}
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}
	if ports[0].Host != 20001 {
		t.Fatalf("unexpected allocated host port: %d", ports[0].Host)
	}
	if ports[1].Host != 20005 {
		t.Fatalf("expected fixed host port 20005, got %d", ports[1].Host)
	}
}

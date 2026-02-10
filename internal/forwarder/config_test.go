package forwarder

import "testing"

func TestParseListeners(t *testing.T) {
	listeners, err := parseListeners(":443=443,:2022=22,:5432=5432,:6379=6379,:9092=9092")
	if err != nil {
		t.Fatalf("parse listeners: %v", err)
	}
	if len(listeners) != 5 {
		t.Fatalf("expected 5 listeners, got %d", len(listeners))
	}
	if listeners[0].Addr != ":443" || listeners[0].GuestPort != 443 {
		t.Fatalf("unexpected first listener: %+v", listeners[0])
	}
}

func TestParseAllowedPorts(t *testing.T) {
	ports, err := parseAllowedPorts("22,443,5432,6379,9092")
	if err != nil {
		t.Fatalf("parse allowed ports: %v", err)
	}
	for _, port := range []int{22, 443, 5432, 6379, 9092} {
		if _, ok := ports[port]; !ok {
			t.Fatalf("expected port %d", port)
		}
	}
}

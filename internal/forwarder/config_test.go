package forwarder

import "testing"

func TestParseListeners(t *testing.T) {
	listeners, err := parseListeners(":8443=8080,:9443=443,:10022=22")
	if err != nil {
		t.Fatalf("parse listeners: %v", err)
	}
	if len(listeners) != 3 {
		t.Fatalf("expected 3 listeners, got %d", len(listeners))
	}
	if listeners[0].Addr != ":8443" || listeners[0].GuestPort != 8080 {
		t.Fatalf("unexpected first listener: %+v", listeners[0])
	}
}

func TestParseAllowedPorts(t *testing.T) {
	ports, err := parseAllowedPorts("22,8080,443")
	if err != nil {
		t.Fatalf("parse allowed ports: %v", err)
	}
	for _, port := range []int{22, 8080, 443} {
		if _, ok := ports[port]; !ok {
			t.Fatalf("expected port %d", port)
		}
	}
}

package unit_test

import (
	"net"
	"testing"

	"git_sonic/pkg/allowlist"
)

func TestAllowlist(t *testing.T) {
	al, err := allowlist.Parse("192.168.1.0/24,10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !al.Allows(net.ParseIP("192.168.1.20")) {
		t.Fatalf("expected allow")
	}
	if !al.Allows(net.ParseIP("10.0.0.1")) {
		t.Fatalf("expected allow")
	}
	if al.Allows(net.ParseIP("10.0.0.2")) {
		t.Fatalf("expected deny")
	}
}

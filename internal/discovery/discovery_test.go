package discovery

import "testing"

func TestHTTPPort(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want int
	}{
		{"default", ":8080", 8080},
		{"custom", ":9090", 9090},
		{"host-port", "192.168.1.10:8088", 8088},
		{"host-only", "192.168.1.10", 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpPort(tt.addr); got != tt.want {
				t.Fatalf("httpPort(%q) = %d, want %d", tt.addr, got, tt.want)
			}
		})
	}
}

func TestAdvertisedProtocol(t *testing.T) {
	t.Setenv("LOCALLINK_TLS_CERT", "")
	t.Setenv("LOCALLINK_TLS_KEY", "")
	if got := advertisedProtocol(); got != "http" {
		t.Fatalf("advertisedProtocol() = %q, want http", got)
	}
	t.Setenv("LOCALLINK_TLS_CERT", "cert.pem")
	t.Setenv("LOCALLINK_TLS_KEY", "key.pem")
	if got := advertisedProtocol(); got != "https" {
		t.Fatalf("advertisedProtocol() = %q, want https", got)
	}
}

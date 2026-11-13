package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"locallink/backend/internal/httpapi"
	"locallink/backend/internal/mdns299"
)

// Bundle 299 N: short ways to open the server, nothing for the user to add.
//   - port 80 next to 8080, so "192.168.18.27/download" works without ":8080"
//   - "linkup.local" answered on the Wi-Fi (multicast DNS, Android 12+, Windows)
// Either one failing only logs PORT80_299 off / MDNS_299 off; 8080 keeps working.

// port80Wanted299: only with the default address (":8080"), plain HTTP, and
// not switched off with LOCALLINK_HTTP80=off.
func port80Wanted299(addr, envHTTP80 string, tlsOnMain bool) bool {
	if tlsOnMain || strings.EqualFold(strings.TrimSpace(envHTTP80), "off") {
		return false
	}
	return strings.TrimSpace(addr) == ":8080"
}

func mdnsName299(env string) string {
	n := strings.ToLower(strings.TrimSpace(env))
	if n == "" {
		return "linkup.local"
	}
	if n == "off" {
		return ""
	}
	if !strings.HasSuffix(n, ".local") {
		n += ".local"
	}
	return n
}

func portOf299(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 8080
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 8080
	}
	return n
}

func startShortNames299(base *http.Server, tlsOnMain bool) func() {
	names := httpapi.ShortNames299{HTTPPort: portOf299(base.Addr)}
	var stops []func()
	if port80Wanted299(base.Addr, getenv("LOCALLINK_HTTP80", ""), tlsOnMain) {
		ln, err := net.Listen("tcp", ":80")
		if err != nil {
			names.Port80Err = err.Error()
			log.Printf("PORT80_299 off: %v", err)
		} else {
			s80 := &http.Server{
				Handler:           base.Handler,
				ReadHeaderTimeout: base.ReadHeaderTimeout,
				ReadTimeout:       base.ReadTimeout,
				WriteTimeout:      base.WriteTimeout,
				IdleTimeout:       base.IdleTimeout,
				MaxHeaderBytes:    base.MaxHeaderBytes,
			}
			names.Port80 = true
			log.Printf("PORT80_299 on (also listening on :80)")
			go func() {
				if err := s80.Serve(ln); err != nil && err != http.ErrServerClosed {
					log.Printf("PORT80_299 stopped: %v", err)
				}
			}()
			stops = append(stops, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = s80.Shutdown(ctx)
			})
		}
	} else {
		log.Printf("PORT80_299 off (address %s)", base.Addr)
	}
	if name := mdnsName299(getenv("LOCALLINK_MDNS_NAME", "")); name != "" {
		stop, err := mdns299.Start(name)
		if err != nil {
			names.MDNSErr = err.Error()
			log.Printf("MDNS_299 off: %v", err)
		} else {
			names.MDNS = true
			names.MDNSName = name
			log.Printf("MDNS_299 on: %s", name)
			stops = append(stops, stop)
		}
	} else {
		log.Printf("MDNS_299 off (LOCALLINK_MDNS_NAME=off)")
	}
	httpapi.SetShortNames299(names)
	return func() {
		for _, s := range stops {
			s()
		}
	}
}

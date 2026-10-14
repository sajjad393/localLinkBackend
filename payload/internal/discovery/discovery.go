package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultPort = 45454
	Request     = "LOCLINK_DISCOVER_V1"
)

type Response struct {
	Service  string `json:"service"`
	Version  string `json:"version"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

func Start(serverAddr string, name string, port int) (func(), error) {
	discoveryPort := DefaultPort
	if raw := strings.TrimSpace(os.Getenv("LOCALLINK_DISCOVERY_PORT")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v < 65536 {
			discoveryPort = v
		}
	}

	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("0.0.0.0:%d", discoveryPort))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}

	// Leave Host empty unless explicitly configured. The Flutter client will
	// then use the UDP response source address, which is the interface that
	// actually reached the phone and avoids accidentally advertising a VPN or
	// Docker interface.
	localHost := strings.TrimSpace(os.Getenv("LOCALLINK_ADVERTISE_HOST"))
	if p := httpPort(serverAddr); p > 0 {
		port = p
	}

	protocol := advertisedProtocol()
	response, _ := json.Marshal(Response{
		Service:  "locallink",
		Version:  "1",
		Host:     localHost,
		Port:     port,
		Name:     name,
		Protocol: protocol,
	})

	go func() {
		buf := make([]byte, 2048)
		for {
			n, remote, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.TrimSpace(string(buf[:n])) != Request {
				continue
			}
			markQuery()
			if _, err := conn.WriteToUDP(response, remote); err != nil {
				log.Printf("discovery response error: %v", err)
			}
		}
	}()

	markStarted(discoveryPort, name, protocol)
	log.Printf("LAN discovery listening on UDP :%d (host=%s)", discoveryPort, localHost)
	return func() { markStopped(); _ = conn.Close() }, nil
}

func httpPort(serverAddr string) int {
	raw := strings.TrimSpace(serverAddr)
	if !strings.Contains(raw, ":") {
		return 80
	}
	if _, p, err := net.SplitHostPort(raw); err == nil {
		if v, err := strconv.Atoi(p); err == nil {
			return v
		}
	}
	return 8080
}

func advertisedProtocol() string {
	if strings.TrimSpace(os.Getenv("LOCALLINK_TLS_CERT")) != "" && strings.TrimSpace(os.Getenv("LOCALLINK_TLS_KEY")) != "" {
		return "https"
	}
	return "http"
}

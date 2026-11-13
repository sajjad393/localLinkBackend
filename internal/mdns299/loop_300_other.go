//go:build !unix && !windows

package mdns299

import "net"

func setLoop300(*net.UDPConn) error { return nil }

//go:build unix

package mdns299

import (
	"net"
	"syscall"
)

// setLoop300 turns multicast loopback back on (Bundle 300).
func setLoop300(c *net.UDPConn) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)
	}); err != nil {
		return err
	}
	return serr
}

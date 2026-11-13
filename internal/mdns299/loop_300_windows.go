//go:build windows

package mdns299

import (
	"net"
	"syscall"
)

// setLoop300 turns multicast loopback back on (Bundle 300). On Windows this
// option works on the receive side: without it this socket never hears the
// questions this PC's own DNS client asks.
func setLoop300(c *net.UDPConn) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)
	}); err != nil {
		return err
	}
	return serr
}

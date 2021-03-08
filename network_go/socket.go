package network_go

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	soReusePort  = 0x0F
	tcpFastOpen  = 0x17
	fastOpenQlen = 16 * 1024
)

func getSockaddr(network, addr string) (sa syscall.Sockaddr, soType int, err error) {
	if network != "tcp4" && network != "tcp6" {
		return nil, -1, errors.New("only tcp4 and tcp6 network is supported")
	}

	tcpAddr, err := net.ResolveTCPAddr(network, addr)
	if err != nil {
		return nil, -1, err
	}

	switch network {
	case "tcp4":
		var sa4 syscall.SockaddrInet4
		sa4.Port = tcpAddr.Port
		copy(sa4.Addr[:], tcpAddr.IP.To4())
		return &sa4, syscall.AF_INET, nil
	case "tcp6":
		var sa6 syscall.SockaddrInet6
		sa6.Port = tcpAddr.Port
		copy(sa6.Addr[:], tcpAddr.IP.To16())
		if tcpAddr.Zone != "" {
			ifi, err := net.InterfaceByName(tcpAddr.Zone)
			if err != nil {
				return nil, -1, err
			}
			sa6.ZoneId = uint32(ifi.Index)
		}
		return &sa6, syscall.AF_INET6, nil
	default:
		return nil, -1, errors.New("Unknown network type " + network)
	}
}

func newSocketCloexec(domain, typ, proto int) (int, error) {
	//SOCK_CLOEXEC子进程中关闭该socket
	fd, err := syscall.Socket(domain, typ|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, proto)
	if err == nil {
		return fd, nil
	}

	if err == syscall.EPROTONOSUPPORT || err == syscall.EINVAL {
		return newSocketCloexecOld(domain, typ, proto)
	}

	return -1, fmt.Errorf("cannot create listening unblocked socket: %s", err)
}

func newSocketCloexecOld(domain, typ, proto int) (int, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Socket(domain, typ, proto)
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return -1, fmt.Errorf("cannot create listening socket: %s", err)
	}
	if err = syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("cannot make non-blocked listening socket: %s", err)
	}
	return fd, nil
}

func enableFastOpen(fd int) error {
	if err := syscall.SetsockoptInt(fd, syscall.SOL_TCP, tcpFastOpen, fastOpenQlen); err != nil {
		return fmt.Errorf("cannot enable TCP_FASTOPEN(qlen=%d): %s", fastOpenQlen, err)
	}
	return nil
}

func soMaxConn() (int, error) {
	//default 128 maxconn
	data, err := ioutil.ReadFile(soMaxConnFilePath)
	if err != nil {
		// This error may trigger on travis build. Just use SOMAXCONN
		if os.IsNotExist(err) {
			return syscall.SOMAXCONN, nil
		}
		return -1, err
	}
	s := strings.TrimSpace(string(data))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return -1, fmt.Errorf("cannot parse somaxconn %q read from %s: %s", s, soMaxConnFilePath, err)
	}

	// Linux stores the backlog in a uint16.
	// Truncate number to avoid wrapping.
	// See https://github.com/golang/go/issues/5030 .
	if n > 1<<16-1 {
		n = 1<<16 - 1
	}
	return n, nil
}

const soMaxConnFilePath = "/proc/sys/net/core/somaxconn"

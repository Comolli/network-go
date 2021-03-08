package network_go

import (
	"errors"
	"net"
	"syscall"
)


type ListenWrapper struct {
	ReusePort   bool
	DeferAccept bool
	FastOpen    bool
	BackLog     int
}

func (lw *ListenWrapper) fdSetup(fd int, socket_addr syscall.Sockaddr, addr string) error {
	var err error
	if err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("cannot enable SO_REUSEADDR: %s", err)
	}

	if err = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1); err != nil {
		return fmt.Errorf("cannot disable Nagle's algorithm: %s", err)
	}

	if lw.ReusePort {
		if err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReusePort, 1); err != nil {
			return fmt.Errorf("cannot enable SO_REUSEPORT: %s", err)
		}
	}

	if lw.DeferAccept {
		if err = enableDeferAccept(fd); err != nil {
			return err
		}
	}

	if lw.FastOpen {
		if err = enableFastOpen(fd); err != nil {
			return err
		}
	}

	if err = syscall.Bind(fd, socket_addr); err != nil {
		return fmt.Errorf("cannot bind to %q: %s", addr, err)
	}
	
	if lw.BackLog <= 0 {
		if backlog, err = soMaxConn(); err != nil {
			return fmt.Errorf("cannot determine backlog to pass to listen(2): %s", err)
		}
	}
	if err = syscall.Listen(fd, lw.BackLog); err != nil {
		return fmt.Errorf("cannot listen on %q: %s", addr, err)
	}
	return nil
}

func (lw *ListenWrapper) NewListenWrapper(network, addr string) (net.Listener, error) { {
	socket_addr, soType, err := getSockaddr(network, addr)
	if err != nil {
		return nil, err
	}
	//full-duplex byte streams
	fd, err := newSocketCloexec(soType, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, err
	}
	if err = lw.fdSetup(fd, socket_addr, addr); err != nil {
		syscall.Close(fd)
		return nil, err
	}

	name := fmt.Sprintf("reuseport.%d.%s.%s", os.Getpid(), network, addr)
	file := os.NewFile(uintptr(fd), name)
	ln, err := net.FileListener(file)
	
	if err != nil {
		file.Close()
		return nil, err
	}

	if err = file.Close(); err != nil {
		ln.Close()
		return nil, err
	}

	return ln, nil
}



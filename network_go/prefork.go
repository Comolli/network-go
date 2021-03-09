package network_go

import (
	"errors"
	"flag"
	"net"
	"os"
	"os/exec"
	"runtime"
)

type Prefork struct {
	Network   string
	Reuseport bool
	// Child prefork processes may exit with failure and will be started over until the times reach
	// the value of RecoverThreshold, then it will return and terminate the server.
	RecoverThreshold  int
	ServeFunc         func(ln net.Listener) error
	ServeTLSFunc      func(ln net.Listener, certFile, keyFile string) error
	ServeTLSEmbedFunc func(ln net.Listener, certData, keyData []byte) error
	ln                net.Listener
	files             []*os.File
}

var preforkChildFlag = "-prefork-child"

func init() { //nolint:gochecknoinits
	// Definition flag to not break the program when the user adds their own flags
	// and runs `flag.Parse()`
	flag.Bool(preforkChildFlag[1:], false, "Is a child process")
}

// IsChild checks if the current thread/process is a child
func IsChild() bool {
	for _, arg := range os.Args[1:] {
		if arg == preforkChildFlag {
			return true
		}
	}

	return false
}
func newListenWrapper() *ListenWrapper {
	return &ListenWrapper{
		ReusePort:   true,
		DeferAccept: true,
		FastOpen:    true,
	}
}

func (p *Prefork) listen(addr string) (net.Listener, error) {
	runtime.GOMAXPROCS(1)

	if p.Network == "" {
		p.Network = "tcp4"
	}

	if p.Reuseport {
		lw := newListenWrapper()
		return lw.NewListenWrapper(p.Network, addr)
	}

	return net.FileListener(os.NewFile(3, ""))
}

func (p *Prefork) setTCPListenerFiles(addr string) error {
	if p.Network == "" {
		p.Network = "tcp4"
	}

	tcpAddr, err := net.ResolveTCPAddr(p.Network, addr)
	if err != nil {
		return err
	}

	tcplistener, err := net.ListenTCP(p.Network, tcpAddr)
	if err != nil {
		return err
	}

	p.ln = tcplistener

	fl, err := tcplistener.File()
	if err != nil {
		return err
	}

	p.files = []*os.File{fl}

	return nil
}

func (p *Prefork) doCommand() (*exec.Cmd, error) {
	/* #nosec G204 */
	cmd := exec.Command(os.Args[0], append(os.Args[1:], preforkChildFlag)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = p.files
	return cmd, cmd.Start()
}

func (p *Prefork) prefork(addr string) (err error) {
	if !p.Reuseport {
		if runtime.GOOS == "windows" {
			return errors.New("ErrOnlyReuseportOnWindows")
		}

		if err = p.setTCPListenerFiles(addr); err != nil {
			return
		}

		// defer for closing the net.Listener opened by setTCPListenerFiles.
		defer func() {
			e := p.ln.Close()
			if err == nil {
				err = e
			}
		}()
	}
	type procSig struct {
		pid int
		err error
	}

	goMaxProcs := runtime.GOMAXPROCS(0)
	sigCh := make(chan procSig, goMaxProcs)
	childProcs := make(map[int]*exec.Cmd)

	defer func() {
		for _, proc := range childProcs {
			_ = proc.Process.Kill()
		}
	}()

	for i := 0; i < goMaxProcs; i++ {
		var cmd *exec.Cmd
		if cmd, err = p.doCommand(); err != nil {
			//p.logger().Printf("failed to start a child prefork process, error: %v\n", err)
			return
		}

		childProcs[cmd.Process.Pid] = cmd
		go func() {
			sigCh <- procSig{cmd.Process.Pid, cmd.Wait()}
		}()
	}

	var exitedProcs int
	for sig := range sigCh {
		delete(childProcs, sig.pid)

		//p.logger().Printf("one of the child prefork processes exited with "+
		//	"error: %v", sig.err)

		if exitedProcs++; exitedProcs > p.RecoverThreshold {
			//p.logger().Printf("child prefork processes exit too many times, "+
			//	"which exceeds the value of RecoverThreshold(%d), "+
			//	"exiting the master process.\n", exitedProcs)
			err = errors.New("ErrOverRecovery")
			break
		}
		var cmd *exec.Cmd
		if cmd, err = p.doCommand(); err != nil {
			break
		}
		childProcs[cmd.Process.Pid] = cmd
		go func() {
			sigCh <- procSig{cmd.Process.Pid, cmd.Wait()}
		}()
	}
	return
}

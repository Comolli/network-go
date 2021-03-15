package zero_copy

import (
	"io"
	"os"
	"syscall"
)

//unidirectional data channel.
type Pipe struct {
	r          *os.File
	w          *os.File
	r_raw_conn syscall.RawConn
	w_raw_conn syscall.RawConn

	teerd   io.Reader
	teepipe *Pipe
}

//create a new pipe
func NewPipe() (*Pipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	w_raw_conn, err := r.SyscallConn()
	if err != nil {
		return nil, err
	}
	w_raw_conn, err := w.SyscallConn()
	if err != nil {
		return nil, err
	}
	return &Pipe{
		r:          r,
		w:          w,
		r_raw_conn: w_raw_conn,
		w_raw_conn: w_raw_conn,
		teerd:      r,
	}, nil
}

func Transfer(dst io.Writer, src io.Reader) (int64, error) {
	return transfer(dst, src)
}

func transfer(dst io.Writer, src io.Reader) (int64, error) {
	// If src is a limited reader, honor the limit.

	rd, limit := get_limiter_reader(src)

	rsc, ok := rd.(syscall.Conn)
	if !ok {
		return io.Copy(dst, src)
	}
	r_raw_conn, err := rsc.SyscallConn()
	if err != nil {
		return io.Copy(dst, src)
	}

	wsc, ok := dst.(syscall.Conn)
	if !ok {
		return io.Copy(dst, src)
	}
	w_raw_conn, err := wsc.SyscallConn()
	if err != nil {
		return io.Copy(dst, src)
	}

	//todo
}

func get_limiter_reader(src io.Reader) (io.Reader, int64) {
	lr, ok := src.(*io.LimitedReader)
	if ok {
		return lr.R, lr.N
	}
	return src, 1<<63 - 1
}

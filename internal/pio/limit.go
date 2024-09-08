package pio

import (
	"context"
	"io"
	"strconv"

	"golang.org/x/time/rate"
)

type LimitReader struct {
	r       io.Reader
	ctx     context.Context
	limiter *rate.Limiter
}

type LimitWriter struct {
	w       io.Writer
	ctx     context.Context
	limiter *rate.Limiter
}

type LimitReadWriter struct {
	rw       io.ReadWriteCloser
	ctx      context.Context
	wlimiter *rate.Limiter
	rlimiter *rate.Limiter
}

func NewLimitReader(r io.Reader, limit int) *LimitReader {
	return &LimitReader{
		r:       r,
		ctx:     context.Background(),
		limiter: rate.NewLimiter(rate.Limit(limit), limit), // set burst = limit
	}
}

func NewLimitWriter(w io.Writer, limit int) *LimitWriter {
	return &LimitWriter{
		w:       w,
		ctx:     context.Background(),
		limiter: rate.NewLimiter(rate.Limit(limit), limit), // set burst = limit
	}
}

func NewLimitReadWriter(rw io.ReadWriteCloser, limit int) *LimitReadWriter {
	return &LimitReadWriter{
		rw:       rw,
		ctx:      context.Background(),
		wlimiter: rate.NewLimiter(rate.Limit(limit), limit), // set burst = limit
		rlimiter: rate.NewLimiter(rate.Limit(limit), limit), // set burst = limit
	}
}

func (s *LimitReadWriter) Read(p []byte) (int, error) {
	if s.rlimiter == nil {
		return s.rw.Read(p)
	}

	do := func(r *LimitReadWriter, p []byte) (int, error) {
		n, err := r.rw.Read(p)
		if err != nil {
			return n, err
		}
		if err := r.rlimiter.WaitN(r.ctx, n); err != nil {
			return n, err
		}
		return n, nil
	}

	if len(p) < s.rlimiter.Burst() {
		return do(s, p)
	}

	burst := s.rlimiter.Burst()
	var read int
	for i := 0; i < len(p); i += burst {
		end := i + burst
		if end > len(p) {
			end = len(p)
		}

		n, err := do(s, p[i:end])
		read += n
		if err != nil {
			return read, err
		}
	}

	return read, nil
}

func (s *LimitReadWriter) Write(p []byte) (int, error) {
	if s.wlimiter == nil {
		return s.rw.Write(p)
	}

	do := func(s *LimitReadWriter, p []byte) (int, error) {
		n, err := s.rw.Write(p)
		if err != nil {
			return n, err
		}

		if err := s.wlimiter.WaitN(context.Background(), n); err != nil {
			return n, err
		}

		return n, nil
	}

	if len(p) < s.wlimiter.Burst() {
		return do(s, p)
	}

	burst := s.wlimiter.Burst()
	var write int
	for i := 0; i < len(p); i += burst {
		end := i + burst
		if end > len(p) {
			end = len(p)
		}
		np := p[i:end]

		n, err := do(s, np)
		write += n
		if err != nil {
			return write, err
		}
	}

	return write, nil
}

func (s *LimitReadWriter) Close() error {
	return s.rw.Close()
}

func (r *LimitReader) Read(p []byte) (int, error) {
	if r.limiter == nil {
		return r.r.Read(p)
	}

	do := func(r *LimitReader, p []byte) (int, error) {
		n, err := r.r.Read(p)
		if err != nil {
			return n, err
		}
		if err := r.limiter.WaitN(r.ctx, n); err != nil {
			return n, err
		}
		return n, nil
	}

	if len(p) < r.limiter.Burst() {
		return do(r, p)
	}

	burst := r.limiter.Burst()
	var read int
	for i := 0; i < len(p); i += burst {
		end := i + burst
		if end > len(p) {
			end = len(p)
		}

		n, err := do(r, p[i:end])
		read += n
		if err != nil {
			return read, err
		}
	}

	return read, nil
}

func (w *LimitWriter) Write(p []byte) (int, error) {
	if w.limiter == nil {
		return w.w.Write(p)
	}

	do := func(w *LimitWriter, p []byte) (int, error) {
		n, err := w.w.Write(p)
		if err != nil {
			return n, err
		}

		if err := w.limiter.WaitN(context.Background(), n); err != nil {
			return n, err
		}

		return n, nil
	}

	if len(p) < w.limiter.Burst() {
		return do(w, p)
	}

	burst := w.limiter.Burst()
	var write int
	for i := 0; i < len(p); i += burst {
		end := i + burst
		if end > len(p) {
			end = len(p)
		}
		np := p[i:end]

		n, err := do(w, np)
		write += n
		if err != nil {
			return write, err
		}
	}

	return write, nil
}

func LimitTransfer(limit string) int {
	inf := 1024 * 1024 * 1024 // just like no limit

	// support b/kb/mb/gb
	if limit[len(limit)-1] != 'b' {
		return inf
	}

	base, err := strconv.Atoi(limit[:len(limit)-2])
	if err != nil {
		return inf
	}

	switch string(limit[len(limit)-2]) {
	case "k":
		return base * 1024
	case "m":
		return base * 1024 * 1024
	case "g":
		return base * 1024 * 1024 * 1024
	default: // number
		return base*10 + int(limit[len(limit)-2]-'0')
	}
}

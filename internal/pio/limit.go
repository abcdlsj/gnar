package pio

import (
	"context"
	"io"
	"strconv"

	"golang.org/x/time/rate"
)

type LimitReadWriter struct {
	rw      io.ReadWriteCloser
	limiter *rate.Limiter
}

func NewLimitReadWriter(rw io.ReadWriteCloser, limit int) *LimitReadWriter {
	return &LimitReadWriter{
		rw:      rw,
		limiter: rate.NewLimiter(rate.Limit(limit), limit),
	}
}

func (s *LimitReadWriter) Read(p []byte) (int, error) {
	n, err := s.rw.Read(p)
	if err != nil {
		return n, err
	}
	if err := s.limiter.WaitN(context.Background(), n); err != nil {
		return n, err
	}
	return n, nil
}

func (s *LimitReadWriter) Write(p []byte) (int, error) {
	n, err := s.rw.Write(p)
	if err != nil {
		return n, err
	}
	if err := s.limiter.WaitN(context.Background(), n); err != nil {
		return n, err
	}
	return n, nil
}

func (s *LimitReadWriter) Close() error {
	return s.rw.Close()
}

func LimitTransfer(limit string) int {
	if len(limit) < 2 || limit[len(limit)-1] != 'b' {
		return 1 << 30
	}

	base, err := strconv.Atoi(limit[:len(limit)-2])
	if err != nil {
		return 1 << 30
	}

	switch limit[len(limit)-2] {
	case 'k':
		return base * 1024
	case 'm':
		return base * 1024 * 1024
	case 'g':
		return base * 1024 * 1024 * 1024
	default:
		return base*10 + int(limit[len(limit)-2]-'0')
	}
}

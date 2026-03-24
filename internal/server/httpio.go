package server

import (
	"bytes"
	"errors"
	"io"
)

var errBodyTooLarge = errors.New("request body exceeds max-body-bytes")

func readBody(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()

	if limit <= 0 {
		return io.ReadAll(body)
	}

	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errBodyTooLarge
	}
	return buf.Bytes(), nil
}

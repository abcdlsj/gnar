package pio

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var (
	globalSecKey = hex.EncodeToString([]byte("dummy secret"))
)

func SetEncryptSecKey(k string) {
	globalSecKey = hex.EncodeToString([]byte(k))
}

func EncryptReader(r io.Reader) (*cipher.StreamReader, error) {
	iv := make([]byte, aes.BlockSize)
	n, err := r.Read(iv)
	if err != nil || n != len(iv) {
		return nil, errors.New("could not read initial value")
	}

	block, err := aes.NewCipher([]byte(globalSecKey))
	if err != nil {
		return nil, err
	}

	stream := cipher.NewOFB(block, iv)
	return &cipher.StreamReader{S: stream, R: r}, nil
}

func EncryptWriter(w io.Writer) (*cipher.StreamWriter, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	n, err := w.Write(iv)
	if err != nil || n != len(iv) {
		return nil, errors.New("could not write initial value")
	}

	block, err := aes.NewCipher([]byte(globalSecKey))
	if err != nil {
		return nil, err
	}

	stream := cipher.NewOFB(block, iv)
	return &cipher.StreamWriter{S: stream, W: w}, nil
}

package pio

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestEncrypt(t *testing.T) {
	t.Logf("key hex: %s", string(globalSecKey))

	str := `Genshin Start!`

	tmpF, _ := os.CreateTemp("", "gnar_testencrypt_")
	w, err := EncryptWriter(tmpF)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte(str)); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(tmpF.Name())

	t.Logf("Encrypted: %s, len: %d", string(data), len(data))
	r, err := EncryptReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	cstr, _ := io.ReadAll(r)
	t.Logf("Decrypted: %s, len: %d", string(cstr), len(cstr))
}

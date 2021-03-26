package dsha256

import (
    "crypto/sha256"
    "encoding/binary"
    "fmt"
)

func DoubleSha256Bytes(blob []byte) ([]byte) {
    h := sha256.New()
    h.Write(blob)
    sum := h.Sum(nil)

    dh := sha256.New()
    dh.Write(sum)
    dsum := dh.Sum(nil)
    return dsum
}

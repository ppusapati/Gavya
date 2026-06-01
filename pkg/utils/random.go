package utils

import (
	"crypto/rand"
	"math/big"
)

func RandomNumber() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return 0, err
	}
	return n.Int64() + 100000, nil
}

// Package nano implements the Nano (XNO) payment protocol for the
// nano:mainnet exact scheme: checksummed accounts, exact raw amounts, the
// order binding a send block carries, and the read-only node query that
// proves a payment. It holds no key and no seed and imports only the
// standard library.
package nano

import (
	"crypto/sha256"
	"net/url"
)

// Network, Scheme and Asset name what this package accepts.
const (
	Network = "nano:mainnet"
	Scheme  = "exact"
	Asset   = "XNO"
)

// Commitment binds one order to one destination and one exact amount:
// SHA-256("xno-binding/v1/rep|" + orderNo + "|" + payTo + "|" + raw).
func Commitment(orderNo, payTo, raw string) [32]byte {
	return sha256.Sum256([]byte("xno-binding/v1/rep|" + orderNo + "|" + payTo + "|" + raw))
}

// CommitmentAccount is the Commitment encoded as a nano_ account. The payer
// sets it as the representative of the send block, so the signed block
// itself names the order it pays for and cannot pay any other.
func CommitmentAccount(orderNo, payTo, raw string) string {
	c := Commitment(orderNo, payTo, raw)
	account, _ := EncodeAccount(c[:]) // 32 bytes, cannot fail
	return account
}

// PaymentURI is the nano: URI a wallet or an agent pays from: the
// destination, the exact amount in raw and the representative that binds
// the send to the order.
func PaymentURI(payTo, raw, representative string) string {
	q := url.Values{}
	q.Set("amount", raw)
	q.Set("representative", representative)
	return "nano:" + payTo + "?" + q.Encode()
}

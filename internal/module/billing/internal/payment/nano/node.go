package nano

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Errors Verify returns. Each one means the block does not pay the order.
var (
	ErrNotConfirmed   = errors.New("nano: block is not a confirmed send")
	ErrWrongPayTo     = errors.New("nano: block does not pay the configured account")
	ErrWrongAmount    = errors.New("nano: block amount is not the exact price")
	ErrNotBound       = errors.New("nano: block representative does not commit to this order")
	ErrInvalidBlockID = errors.New("nano: invalid block hash")
)

var blockHashPattern = regexp.MustCompile(`^[0-9A-F]{64}$`)

// NormalizeBlockHash upper-cases and checks a block hash.
func NormalizeBlockHash(hash string) (string, error) {
	hash = strings.ToUpper(strings.TrimSpace(hash))
	if !blockHashPattern.MatchString(hash) {
		return "", ErrInvalidBlockID
	}
	return hash, nil
}

// maxRPCResponse bounds how much of a node reply is read.
const maxRPCResponse = 64 << 10

// ValidateNodeURL requires an https node RPC URL; plain http is accepted
// only on loopback, where the operator runs their own node.
func ValidateNodeURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("nano: invalid node RPC URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return errors.New("nano: node RPC URL must be https unless it is loopback")
	default:
		return errors.New("nano: invalid node RPC URL scheme")
	}
}

// Block is what a node reports about one block.
type Block struct {
	Hash           string
	Account        string
	Amount         *big.Int
	Confirmed      bool
	Send           bool
	Representative string
	Destination    string
}

// Node reads blocks from a Nano node RPC. It only reads; it never signs or
// publishes anything.
type Node struct {
	URL        string
	HTTPClient *http.Client
}

type blockInfoReply struct {
	Error        string `json:"error"`
	BlockAccount string `json:"block_account"`
	Amount       string `json:"amount"`
	Confirmed    string `json:"confirmed"`
	Subtype      string `json:"subtype"`
	Contents     struct {
		Type           string `json:"type"`
		Representative string `json:"representative"`
		LinkAsAccount  string `json:"link_as_account"`
	} `json:"contents"`
}

// BlockInfo reads one block. A block the node does not know is
// ErrNotConfirmed: it cannot pay anything yet.
func (n *Node) BlockInfo(ctx context.Context, hash string) (*Block, error) {
	hash, err := NormalizeBlockHash(hash)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"action": "block_info", "json_block": "true", "hash": hash})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nano: node RPC: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nano: node RPC status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRPCResponse+1))
	if err != nil {
		return nil, fmt.Errorf("nano: node RPC: %w", err)
	}
	if len(raw) > maxRPCResponse {
		return nil, errors.New("nano: node RPC response too large")
	}
	var info blockInfoReply
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("nano: node RPC: %w", err)
	}
	if info.Error != "" {
		return nil, ErrNotConfirmed
	}
	amount, err := ParseRaw(info.Amount)
	if err != nil {
		return nil, fmt.Errorf("nano: node RPC amount: %w", err)
	}
	return &Block{
		Hash:           hash,
		Account:        info.BlockAccount,
		Amount:         amount,
		Confirmed:      info.Confirmed == "true",
		Send:           info.Subtype == "send" && info.Contents.Type == "state",
		Representative: info.Contents.Representative,
		Destination:    info.Contents.LinkAsAccount,
	}, nil
}

// Expectation is the one payment an order accepts.
type Expectation struct {
	OrderNo string
	PayTo   string
	Raw     *big.Int
}

// Verify proves that block pays exactly the expectation: a confirmed state
// send to PayTo of exactly Raw whose representative commits to OrderNo,
// PayTo and Raw. It does not remember the block; the caller records the
// block hash as the order's only payment, which is what refuses a replay.
func Verify(block *Block, want Expectation) error {
	if block == nil || !block.Confirmed || !block.Send {
		return ErrNotConfirmed
	}
	if want.Raw == nil || want.Raw.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if !SameAccount(block.Destination, want.PayTo) {
		return ErrWrongPayTo
	}
	if block.Amount == nil || block.Amount.Cmp(want.Raw) != 0 {
		return ErrWrongAmount
	}
	if !SameAccount(block.Representative, CommitmentAccount(want.OrderNo, want.PayTo, want.Raw.String())) {
		return ErrNotBound
	}
	if err := ValidateAccount(block.Account); err != nil {
		return fmt.Errorf("nano: block account: %w", err)
	}
	return nil
}

// SameAccount reports whether a and b are valid accounts of the same key,
// whatever their prefix.
func SameAccount(a, b string) bool {
	pa, err := DecodeAccount(a)
	if err != nil {
		return false
	}
	pb, err := DecodeAccount(b)
	if err != nil {
		return false
	}
	return bytes.Equal(pa, pb)
}

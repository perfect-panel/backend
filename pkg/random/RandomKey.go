package random

import (
	cryptorand "crypto/rand"
	"math/big"
	"strings"
)

const (
	chars62 = "E7gLp4jWS6kPv5DzxaY1o9sNcFmBAlUut0ZOhKVM38bqHRJfCwdrTni2QIeXGy"
	base62  = int64(len(chars62))

	chars36 = "6W1HLYPUSJ745ZAKMBQEN9DF8OVGITX320RC"
	base36  = int64(len(chars36))
)

func EncodeBase62(id int64) string {
	if id == 0 {
		return string(chars62[0])
	}

	encoded := ""
	for id > 0 {
		remainder := id % base62
		encoded = string(chars62[remainder]) + encoded
		id /= base62
	}

	index := len(chars62) - 1
	for len(encoded) < 6 {
		encoded = string(chars62[index]) + encoded
		index -= 3
		if index < 0 {
			index = len(chars62) - 1
		}
	}
	// if len(encoded) > 7 {
	// 	encoded = encoded[:7]
	// }

	return encoded
}

// EncodeBase36 ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789
func EncodeBase36(id int64) string {
	if id == 0 {
		return string(chars36[0])
	}

	encoded := ""
	for id > 0 {
		remainder := id % base36
		encoded = string(chars36[remainder]) + encoded
		id /= base36
	}

	index := len(chars36) - 1
	for len(encoded) < 6 {
		encoded = string(chars62[index]) + encoded
		index -= 3
		if index < 0 {
			index = len(chars62) - 1
		}
	}
	// if len(encoded) > 7 {
	// 	encoded = encoded[:7]
	// }

	return encoded
}

func Key(length int, keyType int) string {
	randomString := "0123456789"
	if keyType == 1 {
		randomString = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz"
	}
	return secureKey(length, randomString)
}

func KeyNew(length int, keyType int) string {
	randomString := "0123456789"
	if keyType == 1 {
		randomString = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz"
	} else if keyType == 2 {
		randomString = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	}
	return secureKey(length, randomString)
}

func secureKey(length int, alphabet string) string {
	if length <= 0 || len(alphabet) == 0 {
		return ""
	}
	result := make([]byte, length)
	upper := big.NewInt(int64(len(alphabet)))
	for i := range result {
		index, err := cryptorand.Int(cryptorand.Reader, upper)
		if err != nil {
			// A functioning OS CSPRNG is a security prerequisite. Never fall back
			// to a predictable generator for verification codes or OAuth state.
			panic("crypto/rand unavailable: " + err.Error())
		}
		result[i] = alphabet[index.Int64()]
	}
	return string(result)
}

func StrToDashedString(strNum string) string {
	var result strings.Builder

	for i, ch := range strNum {
		result.WriteRune(ch)
		if (i+1)%4 == 0 && i != len(strNum)-1 {
			result.WriteRune('-')
		}
	}

	return result.String()
}

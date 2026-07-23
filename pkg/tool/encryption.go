package tool

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	PasswordAlgoArgon2id = "argon2id"
	legacyPBKDF2Prefix   = "$pbkdf2-sha512$"
	argon2idPrefix       = "$argon2id$"
)

type pbkdf2Options struct {
	SaltLen      int
	Iterations   int
	KeyLen       int
	HashFunction func() hash.Hash
}

type argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

type argon2idHash struct {
	Params argon2idParams
	Salt   []byte
	Key    []byte
}

var (
	legacyPBKDF2Options = &pbkdf2Options{SaltLen: 16, Iterations: 100, KeyLen: 32, HashFunction: sha512.New}
	defaultArgon2id     = argon2idParams{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}
)

func EncodePassWord(str string) string {
	salt := make([]byte, defaultArgon2id.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		panic(fmt.Errorf("generate password salt: %w", err))
	}
	key := argon2.IDKey([]byte(str), salt, defaultArgon2id.Iterations, defaultArgon2id.Memory, defaultArgon2id.Parallelism, defaultArgon2id.KeyLen)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		defaultArgon2id.Memory,
		defaultArgon2id.Iterations,
		defaultArgon2id.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func VerifyPassWord(passwd, EncodePasswd string) bool {
	if strings.HasPrefix(EncodePasswd, argon2idPrefix) {
		return verifyArgon2id(passwd, EncodePasswd)
	}
	return verifyLegacyPBKDF2(passwd, EncodePasswd)
}

func Md5Encode(str string, isUpper bool) string {
	sum := md5.Sum([]byte(str))
	res := hex.EncodeToString(sum[:])
	//转大写，strings.ToUpper(res)
	if isUpper {
		res = strings.ToUpper(res)
	}
	return res
}

func MultiPasswordVerify(algo, salt, password, hash string) bool {
	if strings.HasPrefix(hash, argon2idPrefix) {
		return verifyArgon2id(password, hash)
	}
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "md5":
		sum := md5.Sum([]byte(password))
		return constantTimeStringEqual(hex.EncodeToString(sum[:]), hash)
	case "sha256":
		sum := sha256.Sum256([]byte(password))
		return constantTimeStringEqual(hex.EncodeToString(sum[:]), hash)
	case "md5salt":
		sum := md5.Sum([]byte(password + salt))
		return constantTimeStringEqual(hex.EncodeToString(sum[:]), hash)
	case "sha256salt":
		// sha256(password + salt), used by SSPanel-style panels (pwdMethod=sha256)
		sum := sha256.Sum256([]byte(password + salt))
		return constantTimeStringEqual(hex.EncodeToString(sum[:]), hash)
	case "default": // PPanel's default algorithm
		return VerifyPassWord(password, hash)
	case PasswordAlgoArgon2id:
		return verifyArgon2id(password, hash)
	case "bcrypt":
		// Bcrypt (corresponding to PHP's password_hash/password_verify)
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil
	}
	return false
}

func PasswordNeedsRehash(algo, hash string) bool {
	parsed, err := parseArgon2idPHC(hash)
	if err != nil {
		return true
	}
	if strings.ToLower(strings.TrimSpace(algo)) != PasswordAlgoArgon2id {
		return true
	}
	return parsed.Params.Memory != defaultArgon2id.Memory ||
		parsed.Params.Iterations != defaultArgon2id.Iterations ||
		parsed.Params.Parallelism != defaultArgon2id.Parallelism ||
		uint32(len(parsed.Salt)) != defaultArgon2id.SaltLen ||
		uint32(len(parsed.Key)) != defaultArgon2id.KeyLen
}

func PasswordAlgoForHash(hash string) string {
	if strings.HasPrefix(hash, argon2idPrefix) {
		return PasswordAlgoArgon2id
	}
	return "default"
}

func verifyLegacyPBKDF2(password, hash string) bool {
	if !strings.HasPrefix(hash, legacyPBKDF2Prefix) {
		return false
	}
	info := strings.Split(hash, "$")
	if len(info) != 4 || info[1] != "pbkdf2-sha512" || info[2] == "" || info[3] == "" {
		return false
	}
	derived := pbkdf2.Key([]byte(password), []byte(info[2]), legacyPBKDF2Options.Iterations, legacyPBKDF2Options.KeyLen, legacyPBKDF2Options.HashFunction)
	return constantTimeStringEqual(hex.EncodeToString(derived), info[3])
}

func verifyArgon2id(password, hash string) bool {
	parsed, err := parseArgon2idPHC(hash)
	if err != nil {
		return false
	}
	key := argon2.IDKey(
		[]byte(password),
		parsed.Salt,
		parsed.Params.Iterations,
		parsed.Params.Memory,
		parsed.Params.Parallelism,
		uint32(len(parsed.Key)),
	)
	return subtle.ConstantTimeCompare(key, parsed.Key) == 1
}

func parseArgon2idPHC(hash string) (*argon2idHash, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != PasswordAlgoArgon2id || parts[2] != "v=19" {
		return nil, errors.New("invalid argon2id PHC format")
	}
	params, err := parseArgon2idParams(parts[3])
	if err != nil {
		return nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return nil, errors.New("invalid argon2id salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return nil, errors.New("invalid argon2id hash")
	}
	params.SaltLen = uint32(len(salt))
	params.KeyLen = uint32(len(key))
	return &argon2idHash{Params: params, Salt: salt, Key: key}, nil
}

func parseArgon2idParams(raw string) (argon2idParams, error) {
	const (
		maxMemoryKiB  = 256 * 1024
		maxIterations = 20
		maxParallel   = 8
	)
	var params argon2idParams
	seen := make(map[string]struct{}, 3)
	for _, item := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return params, errors.New("invalid argon2id parameter")
		}
		if _, ok := seen[key]; ok {
			return params, errors.New("duplicate argon2id parameter")
		}
		seen[key] = struct{}{}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return params, errors.New("invalid argon2id parameter value")
		}
		switch key {
		case "m":
			params.Memory = uint32(n)
		case "t":
			params.Iterations = uint32(n)
		case "p":
			if n > 255 {
				return params, errors.New("invalid argon2id parallelism")
			}
			params.Parallelism = uint8(n)
		default:
			return params, errors.New("unknown argon2id parameter")
		}
	}
	if params.Memory == 0 || params.Memory > maxMemoryKiB ||
		params.Iterations == 0 || params.Iterations > maxIterations ||
		params.Parallelism == 0 || params.Parallelism > maxParallel {
		return params, errors.New("argon2id parameters out of range")
	}
	return params, nil
}

func constantTimeStringEqual(actual, expected string) bool {
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

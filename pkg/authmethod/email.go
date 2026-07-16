package authmethod

import (
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
)

var (
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrEmailDomainDenied  = errors.New("email domain is not allowed")
	ErrEmailDomainListNil = errors.New("email domain allowlist is empty")
)

// ValidateEmail canonicalizes and validates an email address. When domain
// filtering is enabled, the address domain must match an allowlisted domain or
// one of its subdomains at a label boundary.
func ValidateEmail(email, domainList string, enforceDomain bool) (string, error) {
	canonical := CanonicalEmail(email)
	if canonical == "" || len(canonical) > 254 || strings.Count(canonical, "@") != 1 {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(canonical)
	if err != nil || CanonicalEmail(parsed.Address) != canonical {
		return "", ErrInvalidEmail
	}

	parts := strings.SplitN(canonical, "@", 2)
	if len(parts[0]) == 0 || len(parts[0]) > 64 || !validDomain(parts[1]) {
		return "", ErrInvalidEmail
	}
	if !enforceDomain {
		return canonical, nil
	}

	allowed := parseDomainList(domainList)
	if len(allowed) == 0 {
		return "", ErrEmailDomainListNil
	}
	domain := strings.ToLower(strings.TrimSuffix(parts[1], "."))
	for _, candidate := range allowed {
		if domain == candidate || strings.HasSuffix(domain, "."+candidate) {
			return canonical, nil
		}
	}
	return "", ErrEmailDomainDenied
}

func parseDomainList(value string) []string {
	var raw []string
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &raw)
	}
	if len(raw) == 0 {
		raw = strings.FieldsFunc(value, func(r rune) bool {
			switch r {
			case ',', ';', '\n', '\r', '\t', ' ':
				return true
			default:
				return false
			}
		})
	}

	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.ToLower(strings.TrimSpace(item))
		item = strings.TrimPrefix(item, "@")
		item = strings.TrimPrefix(item, "*.")
		item = strings.TrimPrefix(item, ".")
		item = strings.TrimSuffix(item, ".")
		if !validDomain(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func validDomain(domain string) bool {
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, "[]/\\:@") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

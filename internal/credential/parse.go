package credential

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeRotate Mode = "rotate"
	ModeSticky Mode = "sticky"
)

const (
	MinTTLMinutes = 1
	MaxTTLMinutes = 180
	DefaultTTL    = 10
	SIDMinLen     = 4
	SIDMaxLen     = 64
)

var (
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidAccount  = errors.New("invalid account name")
	ErrInvalidSID      = errors.New("invalid session id")
)

var (
	accountRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	sidRe     = regexp.MustCompile(`^[A-Za-z0-9]{4,64}$`)
	midRe     = regexp.MustCompile(`^(.+)-mid_([0-9a-fA-F]{64})$`)
)

var keywords = []string{"sid", "time", "mode", "zone", "st", "city"}

var reservedSubstrings = []string{"_sid_", "_time_", "_mode_", "_zone_"}

// Parsed is a username after protocol decoding, before account defaults.
type Parsed struct {
	Account    string
	SID        string
	TTLMinutes int  // raw value; ApplyDefaults clamps
	TTLSet     bool // true if _time_ was present
	Mode       Mode // empty = infer
	Raw        string
}

func Parse(raw string) (Parsed, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Parsed{}, ErrInvalidUsername
	}
	if m := midRe.FindStringSubmatch(raw); m != nil {
		if err := ValidAccountName(m[1]); err != nil {
			return Parsed{}, err
		}
		return Parsed{
			Account: m[1],
			SID:     strings.ToLower(m[2]),
			Mode:    ModeSticky,
			Raw:     raw,
		}, nil
	}

	account, rest := splitAccount(raw)
	if err := ValidAccountName(account); err != nil {
		return Parsed{}, err
	}
	p := Parsed{Account: account, Raw: raw}
	if rest == "" {
		return p, nil
	}
	fields, err := parseFields(rest)
	if err != nil {
		return Parsed{}, err
	}
	if v := fields["sid"]; v != "" {
		if !ValidSID(v) {
			return Parsed{}, ErrInvalidSID
		}
		p.SID = v
	}
	if v := fields["time"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Parsed{}, ErrInvalidUsername
		}
		p.TTLMinutes = n
		p.TTLSet = true
	}
	if v := fields["mode"]; v != "" {
		switch Mode(strings.ToLower(v)) {
		case ModeRotate, ModeSticky:
			p.Mode = Mode(strings.ToLower(v))
		default:
			return Parsed{}, ErrInvalidUsername
		}
	}
	return p, nil
}

// ApplyDefaults fills mode and TTL from the account. Rotate drops sid for
// session lookup (the original SID remains on Parsed.SID only if mode is sticky).
func ApplyDefaults(p Parsed, defaultMode Mode, defaultTTL, maxTTL int) Parsed {
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	if maxTTL <= 0 || maxTTL > MaxTTLMinutes {
		maxTTL = MaxTTLMinutes
	}
	if defaultMode != ModeSticky {
		defaultMode = ModeRotate
	}

	if p.Mode == "" {
		if p.SID != "" {
			p.Mode = ModeSticky
		} else {
			p.Mode = defaultMode
		}
	}
	if p.Mode == ModeRotate {
		p.SID = ""
	}

	if !p.TTLSet {
		p.TTLMinutes = defaultTTL
	}
	if p.TTLMinutes < MinTTLMinutes {
		p.TTLMinutes = MinTTLMinutes
	}
	if p.TTLMinutes > maxTTL {
		p.TTLMinutes = maxTTL
	}
	return p
}

func ValidAccountName(name string) error {
	if name == "" || !accountRe.MatchString(name) {
		return ErrInvalidAccount
	}
	lower := strings.ToLower(name)
	for _, s := range reservedSubstrings {
		if strings.Contains(lower, s) {
			return ErrInvalidAccount
		}
	}
	return nil
}

func ValidSID(sid string) bool {
	return sidRe.MatchString(sid)
}

func FormatUsername(account, sid string, ttlMin int, mode Mode) string {
	if mode == ModeRotate || sid == "" {
		return account
	}
	if ttlMin <= 0 {
		ttlMin = DefaultTTL
	}
	return fmt.Sprintf("%s_sid_%s_time_%d", account, sid, ttlMin)
}

func RandomSID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 100000000
	return fmt.Sprintf("%08d", n), nil
}

func splitAccount(raw string) (account, rest string) {
	lowest := -1
	for _, kw := range keywords {
		token := "_" + kw + "_"
		if i := strings.Index(raw, token); i >= 0 && (lowest < 0 || i < lowest) {
			lowest = i
		}
	}
	if lowest < 0 {
		return raw, ""
	}
	return raw[:lowest], raw[lowest:]
}

func parseFields(rest string) (map[string]string, error) {
	out := make(map[string]string)
	s := rest
	for s != "" {
		if !strings.HasPrefix(s, "_") {
			return nil, ErrInvalidUsername
		}
		s = s[1:]
		kw, ok := matchKeyword(s)
		if !ok {
			return nil, ErrInvalidUsername
		}
		s = s[len(kw):]
		if !strings.HasPrefix(s, "_") {
			return nil, ErrInvalidUsername
		}
		s = s[1:]
		next := nextKeywordIndex(s)
		var val string
		if next < 0 {
			val = s
			s = ""
		} else {
			val = s[:next]
			s = s[next:]
		}
		if val == "" {
			return nil, ErrInvalidUsername
		}
		out[kw] = val
	}
	return out, nil
}

func matchKeyword(s string) (string, bool) {
	for _, kw := range keywords {
		if strings.HasPrefix(s, kw) {
			return kw, true
		}
	}
	return "", false
}

func nextKeywordIndex(s string) int {
	lowest := -1
	for _, kw := range keywords {
		token := "_" + kw + "_"
		if i := strings.Index(s, token); i >= 0 && (lowest < 0 || i < lowest) {
			lowest = i
		}
	}
	return lowest
}

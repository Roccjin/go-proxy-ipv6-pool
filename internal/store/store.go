package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"ipv6-proxy/internal/credential"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNotFound = errors.New("account not found")
	ErrDisabled = errors.New("account disabled")
	ErrBadPass  = errors.New("invalid password")
	ErrExists   = errors.New("account already exists")
)

var bcryptCost = bcrypt.DefaultCost

func SetBcryptCost(cost int) { bcryptCost = cost }

type Account struct {
	Username    string
	PassHash    string
	Enabled     bool
	DefaultMode credential.Mode
	DefaultTTL  int
	MaxTTL      int
	RateLimit   int
	CreatedAt   int64
}

type StickySession struct {
	Account    string
	SID        string
	ExitIP     string
	Prefix     string
	TTLMin     int
	CreatedAt  int64
	LastSeen   int64
	Hits       int64
	TTLRemain  time.Duration
}

type Store struct {
	rdb    redis.Cmdable
	prefix string
}

func Open(ctx context.Context, redisURL, keyPrefix string) (*Store, *redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("redis url: %w", err)
	}
	cli := redis.NewClient(opt)
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, nil, fmt.Errorf("redis ping: %w", err)
	}
	return New(cli, keyPrefix), cli, nil
}

func New(rdb redis.Cmdable, keyPrefix string) *Store {
	if keyPrefix == "" {
		keyPrefix = "ipv6p:"
	}
	if !strings.HasSuffix(keyPrefix, ":") {
		keyPrefix += ":"
	}
	return &Store{rdb: rdb, prefix: keyPrefix}
}

func (s *Store) key(parts ...string) string {
	return s.prefix + strings.Join(parts, ":")
}

func (s *Store) GetAccount(ctx context.Context, user string) (*Account, error) {
	m, err := s.rdb.HGetAll(ctx, s.key("acct", user)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	a := &Account{
		Username:    user,
		PassHash:    m["pass_hash"],
		Enabled:     m["enabled"] != "0",
		DefaultMode: credential.Mode(m["default_mode"]),
		DefaultTTL:  atoi(m["default_ttl"]),
		MaxTTL:      atoi(m["max_ttl"]),
		RateLimit:   atoi(m["rate_limit"]),
		CreatedAt:   atoi64(m["created_at"]),
	}
	if a.DefaultMode != credential.ModeSticky {
		a.DefaultMode = credential.ModeRotate
	}
	if a.DefaultTTL <= 0 {
		a.DefaultTTL = credential.DefaultTTL
	}
	if a.MaxTTL <= 0 {
		a.MaxTTL = credential.MaxTTLMinutes
	}
	return a, nil
}

func (s *Store) Authenticate(ctx context.Context, user, pass string) (*Account, error) {
	a, err := s.GetAccount(ctx, user)
	if err != nil {
		return nil, err
	}
	if !a.Enabled {
		return nil, ErrDisabled
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PassHash), []byte(pass)) != nil {
		return nil, ErrBadPass
	}
	return a, nil
}

func (s *Store) CreateAccount(ctx context.Context, a Account, password string) error {
	if err := credential.ValidAccountName(a.Username); err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("password required")
	}
	_, err := s.GetAccount(ctx, a.Username)
	if err == nil {
		return ErrExists
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	a.PassHash = string(hash)
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	normalizeAccount(&a)
	return s.putAccount(ctx, a)
}

func (s *Store) EnsureAccount(ctx context.Context, user, pass string, defaults Account) error {
	_, err := s.GetAccount(ctx, user)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	defaults.Username = user
	return s.CreateAccount(ctx, defaults, pass)
}

func (s *Store) UpdateAccount(ctx context.Context, user string, mut func(*Account) error, newPassword string) error {
	a, err := s.GetAccount(ctx, user)
	if err != nil {
		return err
	}
	if mut != nil {
		if err := mut(a); err != nil {
			return err
		}
	}
	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
		if err != nil {
			return err
		}
		a.PassHash = string(hash)
	}
	normalizeAccount(a)
	return s.putAccount(ctx, *a)
}

func (s *Store) DeleteAccount(ctx context.Context, user string) error {
	sessions, err := s.ListStickyByAccount(ctx, user)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		_ = s.DeleteSticky(ctx, user, sess.SID)
	}
	if err := s.rdb.Del(ctx, s.key("acct", user)).Err(); err != nil {
		return err
	}
	return s.rdb.SRem(ctx, s.key("accts"), user).Err()
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	names, err := s.rdb.SMembers(ctx, s.key("accts")).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(names))
	for _, n := range names {
		a, err := s.GetAccount(ctx, n)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				_ = s.rdb.SRem(ctx, s.key("accts"), n).Err()
				continue
			}
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

func (s *Store) putAccount(ctx context.Context, a Account) error {
	if err := s.rdb.HSet(ctx, s.key("acct", a.Username), map[string]any{
		"pass_hash":    a.PassHash,
		"enabled":      bool01(a.Enabled),
		"default_mode": string(a.DefaultMode),
		"default_ttl":  a.DefaultTTL,
		"max_ttl":      a.MaxTTL,
		"rate_limit":   a.RateLimit,
		"created_at":   a.CreatedAt,
	}).Err(); err != nil {
		return err
	}
	return s.rdb.SAdd(ctx, s.key("accts"), a.Username).Err()
}

const stickyLua = `
local v = redis.call('HGET', KEYS[1], 'exit_ip')
if v then
  redis.call('HINCRBY', KEYS[1], 'hits', 1)
  redis.call('HSET', KEYS[1], 'last_seen', ARGV[3])
  return v
end
redis.call('HSET', KEYS[1],
  'exit_ip', ARGV[1],
  'hits', 1,
  'created_at', ARGV[3],
  'last_seen', ARGV[3],
  'ttl_min', ARGV[4],
  'prefix', ARGV[5])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[6])
return ARGV[1]
`

func (s *Store) GetOrCreateSticky(ctx context.Context, account, sid, candidateIP, prefix string, ttl time.Duration, ttlMin int) (string, error) {
	if ttl <= 0 {
		ttl = time.Duration(ttlMin) * time.Minute
	}
	now := time.Now().Unix()
	res, err := s.rdb.Eval(ctx, stickyLua, []string{
		s.key("sess", account, sid),
		s.key("sessidx", account),
	}, candidateIP, int(ttl.Seconds()), now, ttlMin, prefix, sid).Result()
	if err != nil {
		return "", err
	}
	switch v := res.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return "", fmt.Errorf("unexpected sticky result %T", res)
	}
}

func (s *Store) ListStickySessions(ctx context.Context) ([]StickySession, error) {
	names, err := s.rdb.SMembers(ctx, s.key("accts")).Result()
	if err != nil {
		return nil, err
	}
	var out []StickySession
	for _, n := range names {
		list, err := s.ListStickyByAccount(ctx, n)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

func (s *Store) ListStickyByAccount(ctx context.Context, account string) ([]StickySession, error) {
	sids, err := s.rdb.ZRevRange(ctx, s.key("sessidx", account), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]StickySession, 0, len(sids))
	for _, sid := range sids {
		sess, err := s.getSticky(ctx, account, sid)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				_ = s.rdb.ZRem(ctx, s.key("sessidx", account), sid).Err()
				continue
			}
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, nil
}

func (s *Store) DeleteSticky(ctx context.Context, account, sid string) error {
	if err := s.rdb.Del(ctx, s.key("sess", account, sid)).Err(); err != nil {
		return err
	}
	return s.rdb.ZRem(ctx, s.key("sessidx", account), sid).Err()
}

func (s *Store) ClearSticky(ctx context.Context) error {
	accts, err := s.rdb.SMembers(ctx, s.key("accts")).Result()
	if err != nil {
		return err
	}
	for _, a := range accts {
		list, err := s.ListStickyByAccount(ctx, a)
		if err != nil {
			return err
		}
		for _, sess := range list {
			if err := s.DeleteSticky(ctx, a, sess.SID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) StickyCount(ctx context.Context) (int, error) {
	list, err := s.ListStickySessions(ctx)
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

func (s *Store) getSticky(ctx context.Context, account, sid string) (*StickySession, error) {
	key := s.key("sess", account, sid)
	m, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 || m["exit_ip"] == "" {
		return nil, ErrNotFound
	}
	ttl, err := s.rdb.TTL(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	return &StickySession{
		Account:   account,
		SID:       sid,
		ExitIP:    m["exit_ip"],
		Prefix:    m["prefix"],
		TTLMin:    atoi(m["ttl_min"]),
		CreatedAt: atoi64(m["created_at"]),
		LastSeen:  atoi64(m["last_seen"]),
		Hits:      atoi64(m["hits"]),
		TTLRemain: ttl,
	}, nil
}

func normalizeAccount(a *Account) {
	if a.DefaultMode != credential.ModeSticky {
		a.DefaultMode = credential.ModeRotate
	}
	if a.DefaultTTL <= 0 {
		a.DefaultTTL = credential.DefaultTTL
	}
	if a.MaxTTL <= 0 || a.MaxTTL > credential.MaxTTLMinutes {
		a.MaxTTL = credential.MaxTTLMinutes
	}
	if a.DefaultTTL > a.MaxTTL {
		a.DefaultTTL = a.MaxTTL
	}
}

func bool01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

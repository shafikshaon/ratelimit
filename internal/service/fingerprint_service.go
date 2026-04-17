package service

// Fingerprint design rationale
// ─────────────────────────────
// Constraints:
//   • Tokens must be long-lived (hours/days, not seconds).
//   • Client IPs must NOT be stored or used for binding (privacy regulation).
//
// Remaining attack surface after removing IP binding + short TTL:
//   If an attacker can extract an fp_token cookie they get a window equal to the TTL.
//   HttpOnly cookies cannot be read by JavaScript, so extraction requires
//   OS-level access to the browser's encrypted cookie store — a very high bar.
//
// Defence-in-depth without IP:
//   1.  HMAC-signed nonce        — cannot be pre-computed.
//   2.  Proof of Work            — 3 leading zero bytes ≈ 8M SHA-256 hashes per token.
//   3.  Collection-time gate     — nonce must be consumed within 55 s of issuance.
//   4.  Bot-signal gate          — navigator.webdriver=true or outerDims 0×0 → rejected.
//   5.  Double-cookie binding    — token HMAC includes a server-generated session_id
//                                  that lives in a SEPARATE httpOnly cookie (fp_sid).
//                                  An attacker must steal BOTH independent cookies;
//                                  same attack surface as IP binding but without storing IP.
//   6.  Per-fingerprint rate limit — max N new tokens per fp_hash per hour.
//                                   Limits bulk session farming from one device.
//   7.  Origin enforcement       — requests without a matching Origin header rejected.
//   8.  Long-lived but revocable — session stored in Redis; operator can delete the key
//                                  (e.g. fp:session:<session_id>) to invalidate any session.
//   9.  Token is NOT single-use  — avoids the usability problem of per-request tokens.
//                                  Reuse is bounded by the TTL and the rate-limit on issuance.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	fpNonceTTL         = 60 * time.Second
	fpNoncePrefix      = "fp:nonce:"
	fpSessionPrefix    = "fp:session:" // session_id → fp_hash (for revocation + validation)
	fpRatePrefix       = "fp:rate:"    // per-fp_hash issuance rate counter
	fpRateWindow       = time.Hour
	fpRateMax          = 10 // max new sessions per unique fingerprint per hour

	fpReqPrefix = "fp:req:" // single-use per-request token
	fpReqTTL    = 30 * time.Second

	// PowDifficulty: leading zero bytes required in SHA-256(nonce+solution).
	// Difficulty 2 = 65 536 average hashes: ~65ms in browser JS, ~130ms in Python.
	// The real throughput cap is the per-fingerprint rate limit (10 sessions/hr),
	// not the PoW cost alone. Difficulty 2 keeps UX instant while still adding
	// meaningful friction against bulk automated token farming.
	PowDifficulty      = 2
	maxCollectionDelay = 55 * time.Second
)

var (
	errInvalidNonceHMAC   = errors.New("fingerprint: invalid nonce signature")
	errNonceExpired       = errors.New("fingerprint: nonce expired or already used")
	errInvalidToken       = errors.New("fingerprint: malformed token")
	errTokenExpired       = errors.New("fingerprint: token expired")
	errSessionNotFound    = errors.New("fingerprint: session not found or revoked")
	errSessionMismatch    = errors.New("fingerprint: session cookie does not match token")
	errInvalidTokenHMAC   = errors.New("fingerprint: invalid token signature")
	errPowFailed          = errors.New("fingerprint: proof-of-work verification failed")
	errBotDetected        = errors.New("fingerprint: automated client detected")
	errCollectionTooLate  = errors.New("fingerprint: fingerprint collected outside allowed window")
	ErrRateLimited        = errors.New("fingerprint: too many token requests from this fingerprint")
	ErrReqTokenInvalid    = errors.New("fingerprint: request token not found, already used, or expired")
	ErrReqTokenMismatch   = errors.New("fingerprint: request token does not belong to this session")
)

// BotSignals are browser properties verified server-side.
type BotSignals struct {
	Webdriver    bool `json:"webdriver"`
	OuterWidth   int  `json:"outer_width"`
	OuterHeight  int  `json:"outer_height"`
	PluginCount  int  `json:"plugin_count"`
	ChromeObject bool `json:"chrome_object"`
}

// TokenRequest carries everything the server needs to issue a session token.
type TokenRequest struct {
	Nonce       string
	HMACNonce   string
	FPHash      string
	PowSolution string
	CollectedAt int64
	BotSignals  BotSignals
}

// SessionResult is returned by IssueToken and contains both the token and session_id
// so the handler can set two separate httpOnly cookies.
type SessionResult struct {
	Token     string
	SessionID string
	TTL       time.Duration
}

// FingerprintService issues and verifies long-lived browser fingerprint sessions.
type FingerprintService struct {
	redis  *redis.Client
	secret []byte
	ttl    time.Duration
}

func NewFingerprintService(client *redis.Client, secret []byte, ttlHours int) *FingerprintService {
	if ttlHours <= 0 {
		ttlHours = 24
	}
	return &FingerprintService{
		redis:  client,
		secret: secret,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// IssueChallenge generates a random nonce, stores it in Redis for 60 s, and
// returns the nonce, its HMAC, and the required PoW difficulty.
func (s *FingerprintService) IssueChallenge(ctx context.Context) (nonce, hmacNonce string, difficulty int, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", 0, fmt.Errorf("fingerprint: generate nonce: %w", err)
	}
	nonce = hex.EncodeToString(raw)
	hmacNonce = s.sign(nonce)

	issuedAt := strconv.FormatInt(time.Now().Unix(), 10)
	meta, _ := json.Marshal(map[string]string{"issued_at": issuedAt})
	if err = s.redis.Set(ctx, fpNoncePrefix+nonce, meta, fpNonceTTL).Err(); err != nil {
		return "", "", 0, fmt.Errorf("fingerprint: store nonce: %w", err)
	}
	return nonce, hmacNonce, PowDifficulty, nil
}

// IssueToken validates all defence layers and returns a SessionResult containing:
//   - Token:     HMAC-signed, includes session_id → set as httpOnly fp_token cookie
//   - SessionID: random, opaque, stored in Redis → set as httpOnly fp_sid cookie
//
// Both cookies must be present and matching on every /check request.
func (s *FingerprintService) IssueToken(ctx context.Context, req TokenRequest) (*SessionResult, error) {
	// Layer 1: nonce HMAC.
	if !hmac.Equal([]byte(req.HMACNonce), []byte(s.sign(req.Nonce))) {
		return nil, errInvalidNonceHMAC
	}

	// Layer 2: consume nonce atomically.
	stored, err := s.redis.GetDel(ctx, fpNoncePrefix+req.Nonce).Result()
	if err == redis.Nil || stored == "" {
		return nil, errNonceExpired
	}
	if err != nil {
		return nil, fmt.Errorf("fingerprint: consume nonce: %w", err)
	}

	// Layer 3: collection-time gate.
	var meta map[string]string
	if err := json.Unmarshal([]byte(stored), &meta); err == nil {
		if issuedAtStr, ok := meta["issued_at"]; ok {
			if issuedAt, err := strconv.ParseInt(issuedAtStr, 10, 64); err == nil {
				if time.Since(time.Unix(issuedAt, 0)) > maxCollectionDelay {
					return nil, errCollectionTooLate
				}
			}
		}
	}

	// Layer 4: Proof of Work.
	if !verifyPoW(req.Nonce, req.PowSolution, PowDifficulty) {
		return nil, errPowFailed
	}

	// Layer 5: bot signal gate.
	if req.BotSignals.Webdriver {
		return nil, errBotDetected
	}
	if req.BotSignals.OuterWidth == 0 && req.BotSignals.OuterHeight == 0 {
		return nil, errBotDetected
	}

	// Layer 6: per-fingerprint rate limit (no IP used).
	rateKey := fpRatePrefix + req.FPHash
	count, err := s.redis.Incr(ctx, rateKey).Result()
	if err != nil {
		return nil, fmt.Errorf("fingerprint: rate check: %w", err)
	}
	if count == 1 {
		s.redis.Expire(ctx, rateKey, fpRateWindow)
	}
	if count > fpRateMax {
		return nil, ErrRateLimited
	}

	// Layer 7: generate session_id — stored in Redis and embedded in token HMAC.
	sidRaw := make([]byte, 32)
	if _, err = rand.Read(sidRaw); err != nil {
		return nil, fmt.Errorf("fingerprint: generate session_id: %w", err)
	}
	sessionID := hex.EncodeToString(sidRaw)

	// Store fp_hash in Redis keyed by session_id.
	// This enables session revocation: delete fp:session:<id> to invalidate.
	if err := s.redis.Set(ctx, fpSessionPrefix+sessionID, req.FPHash, s.ttl).Err(); err != nil {
		return nil, fmt.Errorf("fingerprint: store session: %w", err)
	}

	// Build token: fp_hash|issued_at|session_id|sig
	// session_id is in the HMAC — matching the fp_sid cookie is required.
	issuedAt := strconv.FormatInt(time.Now().Unix(), 10)
	payload := strings.Join([]string{req.FPHash, issuedAt, sessionID}, "|")
	sig := s.sign(payload)
	token := base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))

	return &SessionResult{Token: token, SessionID: sessionID, TTL: s.ttl}, nil
}

// VerifyToken decodes and validates the token, then checks the sessionID from
// the companion fp_sid cookie. Both must match — neither is useful alone.
func (s *FingerprintService) VerifyToken(ctx context.Context, token, sessionID string) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return errInvalidToken
	}

	// token payload: fp_hash|issued_at|session_id|sig
	parts := strings.SplitN(string(raw), "|", 4)
	if len(parts) != 4 {
		return errInvalidToken
	}
	fpHash, issuedAtStr, tokenSessionID, sig := parts[0], parts[1], parts[2], parts[3]

	// Verify HMAC.
	payload := strings.Join([]string{fpHash, issuedAtStr, tokenSessionID}, "|")
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return errInvalidTokenHMAC
	}

	// Verify the fp_sid cookie matches what's in the token.
	// An attacker who steals only fp_token cannot forge fp_sid, and vice versa.
	if tokenSessionID != sessionID {
		return errSessionMismatch
	}

	// Verify timestamp within TTL.
	tsUnix, err := strconv.ParseInt(issuedAtStr, 10, 64)
	if err != nil {
		return errInvalidToken
	}
	age := time.Since(time.Unix(tsUnix, 0))
	if age > s.ttl || age < -5*time.Second {
		return errTokenExpired
	}

	// Verify session still exists in Redis (enables server-side revocation).
	storedFPHash, err := s.redis.Get(ctx, fpSessionPrefix+sessionID).Result()
	if err == redis.Nil {
		return errSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("fingerprint: check session: %w", err)
	}
	// Cross-check: fp_hash in token must match what was stored at issuance.
	if storedFPHash != fpHash {
		return errSessionMismatch
	}
	return nil
}

// RevokeSession deletes the session from Redis, immediately invalidating the token.
// Operators can call this to force re-fingerprinting for a specific session.
func (s *FingerprintService) RevokeSession(ctx context.Context, sessionID string) error {
	return s.redis.Del(ctx, fpSessionPrefix+sessionID).Err()
}

// IssueRequestToken generates a random single-use token tied to sessionID.
// The token expires in fpReqTTL (30 s). The caller must have already verified
// the session (VerifyToken) before calling this.
//
// The token is returned in the JSON response body so the browser JS can include
// it as X-Request-Token on the next /check request. Because the token is short-
// lived and single-use, a "Copy as cURL" of a /check request immediately fails:
// the token is already consumed by the browser's own request.
func (s *FingerprintService) IssueRequestToken(ctx context.Context, sessionID string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("fingerprint: generate request token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := s.redis.Set(ctx, fpReqPrefix+token, sessionID, fpReqTTL).Err(); err != nil {
		return "", fmt.Errorf("fingerprint: store request token: %w", err)
	}
	return token, nil
}

// VerifyRequestToken atomically consumes the token via GETDEL and checks that
// the stored sessionID matches the expected one. A consumed token cannot be reused.
func (s *FingerprintService) VerifyRequestToken(ctx context.Context, token, sessionID string) error {
	if token == "" {
		return ErrReqTokenInvalid
	}
	stored, err := s.redis.GetDel(ctx, fpReqPrefix+token).Result()
	if err == redis.Nil || stored == "" {
		return ErrReqTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("fingerprint: verify request token: %w", err)
	}
	if stored != sessionID {
		return ErrReqTokenMismatch
	}
	return nil
}

// verifyPoW checks that SHA-256(nonce+solution) has `difficulty` leading zero bytes.
func verifyPoW(nonce, solution string, difficulty int) bool {
	if solution == "" || difficulty <= 0 {
		return false
	}
	h := sha256.Sum256([]byte(nonce + solution))
	prefix := make([]byte, difficulty)
	return bytes.Equal(h[:difficulty], prefix)
}

func (s *FingerprintService) sign(data string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

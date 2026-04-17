package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shafikshaon/ratelimit/internal/dto"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/service"
	"go.uber.org/zap"
)

const maxFPHashLen = 128

// Cookie names.
// fp_token: the HMAC-signed session token (bound to fp_sid via HMAC).
// fp_sid:   the opaque session ID (stored in Redis; used for revocation).
// Both are httpOnly SameSite=Strict. An attacker needs both to impersonate a session.
const (
	fpTokenCookie = "fp_token"
	fpSIDCookie   = "fp_sid"
)

// FingerprintHandler serves the challenge/token endpoints.
type FingerprintHandler struct {
	svc            *service.FingerprintService
	allowedOrigins []string
}

func NewFingerprintHandler(svc *service.FingerprintService, allowedOrigins []string) *FingerprintHandler {
	return &FingerprintHandler{svc: svc, allowedOrigins: allowedOrigins}
}

func (h *FingerprintHandler) originAllowed(c *gin.Context) bool {
	origin := c.GetHeader("Origin")

	// Browsers omit Origin on same-origin GET requests. Fall back to Referer,
	// extracting just the scheme+host so it can be compared to allowed origins.
	if origin == "" {
		if ref := c.GetHeader("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}

	if origin == "" {
		return false
	}
	for _, a := range h.allowedOrigins {
		if strings.EqualFold(origin, a) {
			return true
		}
	}
	return false
}

// Challenge GET /api/v1/fingerprint/challenge
func (h *FingerprintHandler) Challenge(c *gin.Context) {
	if !h.originAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
		return
	}
	nonce, hmacNonce, difficulty, err := h.svc.IssueChallenge(c.Request.Context())
	if err != nil {
		if err == service.ErrRateLimited {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many fingerprint requests"})
			return
		}
		logger.L.Error("fingerprint: issue challenge", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue challenge"})
		return
	}
	c.JSON(http.StatusOK, dto.ChallengeResponse{
		Nonce:      nonce,
		HMACNonce:  hmacNonce,
		Difficulty: difficulty,
		ExpiresIn:  60,
	})
}

// Token POST /api/v1/fingerprint/token
// Sets two httpOnly cookies:
//   - fp_token: HMAC-signed token whose payload includes the session_id.
//   - fp_sid:   opaque session ID (the other half of the binding).
//
// Neither cookie value appears in the JSON response.
// Scripts that copy the request will not receive httpOnly cookies in the response.
func (h *FingerprintHandler) Token(c *gin.Context) {
	if !h.originAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
		return
	}

	var req dto.FingerprintTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Nonce == "" || req.HMACNonce == "" || req.FPHash == "" || req.PowSolution == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nonce, hmac_nonce, fp_hash, and pow_solution are required"})
		return
	}
	if len(req.FPHash) > maxFPHashLen || !isSafeString(req.FPHash) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fp_hash"})
		return
	}

	result, err := h.svc.IssueToken(c.Request.Context(), service.TokenRequest{
		Nonce:       req.Nonce,
		HMACNonce:   req.HMACNonce,
		FPHash:      req.FPHash,
		PowSolution: req.PowSolution,
		CollectedAt: req.CollectedAt,
		BotSignals: service.BotSignals{
			Webdriver:    req.BotSignals.Webdriver,
			OuterWidth:   req.BotSignals.OuterWidth,
			OuterHeight:  req.BotSignals.OuterHeight,
			PluginCount:  req.BotSignals.PluginCount,
			ChromeObject: req.BotSignals.ChromeObject,
		},
	})
	if err != nil {
		if err == service.ErrRateLimited {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many fingerprint requests"})
			return
		}
		logger.L.Debug("fingerprint: issue token failed", zap.Error(err))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	ttlSec := int(result.TTL.Seconds())

	// Cookie 1: fp_token — the HMAC-signed session token.
	// Path=/api/v1/apis limits it to the rate-limit endpoints only.
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     fpTokenCookie,
		Value:    result.Token,
		Path:     "/api/v1/apis",
		MaxAge:   ttlSec,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false, // set true in production behind HTTPS
	})

	// Cookie 2: fp_sid — the opaque session ID.
	// Broader Path=/api/v1 so it's also sent to the fingerprint endpoints
	// if the client wants to refresh or check session status.
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     fpSIDCookie,
		Value:    result.SessionID,
		Path:     "/api/v1",
		MaxAge:   ttlSec,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
	})

	// Return only metadata — never the token or session_id values.
	c.JSON(http.StatusOK, dto.TokenResponse{ExpiresIn: ttlSec})
}

// RequestToken GET /api/v1/fingerprint/request-token
// Requires valid fp_token + fp_sid session cookies. Returns a single-use,
// 30-second request token that must be sent as X-Request-Token on the next
// /check call. This prevents "Copy as cURL" attacks: the token is consumed
// by the original request, so a replayed curl immediately gets 403.
func (h *FingerprintHandler) RequestToken(c *gin.Context) {
	if !h.originAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
		return
	}
	fpToken, fpSID := ReadFPCookies(c)
	if fpToken == "" || fpSID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "fingerprint session required"})
		return
	}
	if err := h.svc.VerifyToken(c.Request.Context(), fpToken, fpSID); err != nil {
		logger.L.Debug("fingerprint: request-token session invalid", zap.Error(err))
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired fingerprint session"})
		return
	}
	reqToken, err := h.svc.IssueRequestToken(c.Request.Context(), fpSID)
	if err != nil {
		logger.L.Error("fingerprint: issue request token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue request token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"request_token": reqToken, "expires_in": 30})
}

// ReadFPCookies reads both fingerprint cookies from the request.
// Returns empty strings if either is missing.
func ReadFPCookies(c *gin.Context) (token, sessionID string) {
	token, _ = c.Cookie(fpTokenCookie)
	sessionID, _ = c.Cookie(fpSIDCookie)
	return token, sessionID
}

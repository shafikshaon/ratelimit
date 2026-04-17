package handler

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/shafikshaon/ratelimit/internal/dto"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/model"
	"github.com/shafikshaon/ratelimit/internal/service"
	"go.uber.org/zap"
)

const (
	maxEmailLen  = 254 // RFC 5321
	maxWalletLen = 100
	maxReasonLen = 500
)

var validWindowUnits = map[string]bool{
	"seconds": true,
	"minutes": true,
	"hours":   true,
	"daily":   true,
}

// isSafeString rejects strings containing control characters or leading/trailing whitespace.
func isSafeString(s string) bool {
	if s != strings.TrimSpace(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// isValidOverrideTier returns true if the value is "", "global", or a positive integer string.
func isValidOverrideTier(v string) bool {
	if v == "" || v == "global" {
		return true
	}
	n, err := strconv.Atoi(v)
	return err == nil && n > 0
}

// APIHandler is a thin HTTP layer that delegates all business logic to APIService.
type APIHandler struct {
	svc *service.APIService
}

func NewAPIHandler(svc *service.APIService) *APIHandler {
	return &APIHandler{svc: svc}
}

// ListAPIs GET /api/v1/apis
func (h *APIHandler) ListAPIs(c *gin.Context) {
	groups, err := h.svc.ListAPIs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch APIs"})
		return
	}
	c.JSON(http.StatusOK, dto.APIListResponse{Data: groups})
}

// GetAPI GET /api/v1/apis/:name
func (h *APIHandler) GetAPI(c *gin.Context) {
	apiName := c.Param("name")
	tiers, err := h.svc.GetTierConfig(c.Request.Context(), apiName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch config"})
		return
	}
	if tiers == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		return
	}
	c.JSON(http.StatusOK, dto.APIDetailResponse{Data: dto.APIDetail{Name: apiName, Tiers: tiers}})
}

// UpdateTier PATCH /api/v1/apis/:name/tiers/:tier
func (h *APIHandler) UpdateTier(c *gin.Context) {
	apiName := c.Param("name")
	tierNum, err := strconv.Atoi(c.Param("tier"))
	if err != nil || tierNum < 1 || tierNum > 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tier must be 1, 2, or 3"})
		return
	}

	var req dto.UpdateTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Debug("UpdateTier bad JSON", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Validate unit.
	if !validWindowUnits[req.Unit] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "window_unit must be one of: seconds, minutes, hours, daily"})
		return
	}
	// Validate window for non-daily units.
	if req.Unit != "daily" {
		if req.Window == nil || *req.Window <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "window must be a positive integer for non-daily units"})
			return
		}
		if *req.Window > 365*24*3600 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "window exceeds maximum allowed value"})
			return
		}
	}
	// Validate max_requests.
	if req.MaxRequests <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_requests must be a positive integer"})
		return
	}
	if req.MaxRequests > 10_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_requests exceeds maximum allowed value"})
		return
	}
	// Validate reset_hour.
	if req.ResetHour < 0 || req.ResetHour > 23 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reset_hour must be 0–23"})
		return
	}

	t := model.Tier{
		Window:      req.Window,
		Unit:        req.Unit,
		MaxRequests: req.MaxRequests,
		ResetHour:   req.ResetHour,
	}
	if err := h.svc.UpdateTier(c.Request.Context(), apiName, tierNum, t); err != nil {
		if service.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tier not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update tier"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// GetWalletConfig GET /api/v1/apis/:name/config/:wallet
func (h *APIHandler) GetWalletConfig(c *gin.Context) {
	apiName := c.Param("name")
	wallet := c.Param("wallet")

	if len(wallet) > maxWalletLen || !isSafeString(wallet) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet"})
		return
	}

	resolved, err := h.svc.GetWalletConfig(c.Request.Context(), apiName, wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch config"})
		return
	}
	if resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		return
	}
	c.JSON(http.StatusOK, resolved)
}

// CheckRequest POST /api/v1/apis/:name/check
func (h *APIHandler) CheckRequest(c *gin.Context) {
	apiName := c.Param("name")

	var req dto.CheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Debug("CheckRequest bad JSON", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Validate email if provided.
	if req.Email != "" {
		if len(req.Email) > maxEmailLen || !isSafeString(req.Email) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email"})
			return
		}
	}
	// Validate wallet if provided.
	if req.Wallet != "" {
		if len(req.Wallet) > maxWalletLen || !isSafeString(req.Wallet) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet"})
			return
		}
	}
	// At least one identifier is required.
	if req.Email == "" && req.Wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email or wallet is required"})
		return
	}

	result, err := h.svc.Check(c.Request.Context(), apiName, req.Email, req.Wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check failed"})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		return
	}

	status := http.StatusOK
	if !result.Allowed {
		status = http.StatusTooManyRequests
	}
	c.JSON(status, result)
}

// GetUsage GET /api/v1/apis/:name/usage?email=&wallet=
func (h *APIHandler) GetUsage(c *gin.Context) {
	apiName := c.Param("name")
	email := c.Query("email")
	wallet := c.Query("wallet")

	if email != "" && (len(email) > maxEmailLen || !isSafeString(email)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email"})
		return
	}
	if wallet != "" && (len(wallet) > maxWalletLen || !isSafeString(wallet)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet"})
		return
	}

	usage, err := h.svc.GetUsage(c.Request.Context(), apiName, email, wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch usage"})
		return
	}
	if usage == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		return
	}
	c.JSON(http.StatusOK, dto.UsageResponse{Data: usage})
}

// ListOverrides GET /api/v1/apis/:name/overrides
func (h *APIHandler) ListOverrides(c *gin.Context) {
	apiName := c.Param("name")
	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	overrides, nextToken, hasMore, err := h.svc.ListOverrides(c.Request.Context(), apiName, limit, c.Query("page_token"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch overrides"})
		return
	}

	resp := dto.OverridePageResponse{Data: overrides, HasMore: hasMore}
	if hasMore {
		resp.NextPageToken = nextToken
	}
	c.JSON(http.StatusOK, resp)
}

// CreateOverride POST /api/v1/apis/:name/overrides
func (h *APIHandler) CreateOverride(c *gin.Context) {
	apiName := c.Param("name")

	var req dto.CreateOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Debug("CreateOverride bad JSON", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet is required"})
		return
	}
	if len(req.Wallet) > maxWalletLen || !isSafeString(req.Wallet) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet"})
		return
	}
	// Validate tier override values: must be "", "global", or a positive integer string.
	// Use a slice (not a map) to guarantee deterministic field order in error messages.
	for _, field := range []string{"t1", "t2", "t3"} {
		var val string
		switch field {
		case "t1":
			val = req.T1
		case "t2":
			val = req.T2
		case "t3":
			val = req.T3
		}
		if !isValidOverrideTier(val) {
			c.JSON(http.StatusBadRequest, gin.H{"error": field + " must be 'global' or a positive integer"})
			return
		}
	}
	if len(req.Reason) > maxReasonLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason exceeds maximum length"})
		return
	}

	o := model.Override{
		Wallet: req.Wallet,
		T1:     req.T1,
		T2:     req.T2,
		T3:     req.T3,
		Reason: req.Reason,
	}
	if err := h.svc.CreateOverride(c.Request.Context(), apiName, o); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create override"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "ok"})
}

// DeleteOverride DELETE /api/v1/apis/:name/overrides/:wallet
func (h *APIHandler) DeleteOverride(c *gin.Context) {
	apiName := c.Param("name")
	wallet := c.Param("wallet")

	if len(wallet) > maxWalletLen || !isSafeString(wallet) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet"})
		return
	}

	if err := h.svc.DeleteOverride(c.Request.Context(), apiName, wallet); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete override"})
		return
	}
	c.Status(http.StatusNoContent)
}

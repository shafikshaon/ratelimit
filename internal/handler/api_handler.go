package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shafikshaon/ratelimit/internal/dto"
	"github.com/shafikshaon/ratelimit/internal/model"
	"github.com/shafikshaon/ratelimit/internal/service"
)

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
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tier number"})
		return
	}

	var req dto.UpdateTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet is required"})
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

	if err := h.svc.DeleteOverride(c.Request.Context(), apiName, wallet); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete override"})
		return
	}
	c.Status(http.StatusNoContent)
}

package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shafikshaon/ratelimit/internal/model"
	"github.com/shafikshaon/ratelimit/internal/repository"
)

type APIHandler struct {
	apiRepo      *repository.APIRepository
	tierRepo     *repository.TierRepository
	overrideRepo *repository.OverrideRepository
}

func NewAPIHandler(
	apiRepo *repository.APIRepository,
	tierRepo *repository.TierRepository,
	overrideRepo *repository.OverrideRepository,
) *APIHandler {
	return &APIHandler{apiRepo: apiRepo, tierRepo: tierRepo, overrideRepo: overrideRepo}
}

// ListAPIs returns all APIs grouped — no tiers or overrides (cheap list for the sidebar).
func (h *APIHandler) ListAPIs(c *gin.Context) {
	groups, err := h.apiRepo.ListGrouped(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch APIs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": groups})
}

// GetAPI returns the tier configuration for a single API (PostgreSQL → Redis cache).
func (h *APIHandler) GetAPI(c *gin.Context) {
	apiName := c.Param("name")
	tiers, err := h.tierRepo.Get(c.Request.Context(), apiName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch config"})
		return
	}
	if tiers == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"name": apiName, "tiers": tiers}})
}

// UpdateTier writes to PostgreSQL and invalidates the Redis cache.
func (h *APIHandler) UpdateTier(c *gin.Context) {
	apiName := c.Param("name")
	tierNum, err := strconv.Atoi(c.Param("tier"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tier number"})
		return
	}

	var tier model.Tier
	if err := c.ShouldBindJSON(&tier); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.tierRepo.UpdateTier(c.Request.Context(), apiName, tierNum, tier); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update tier"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// ListOverrides returns a cursor-paginated page of overrides from ScyllaDB.
func (h *APIHandler) ListOverrides(c *gin.Context) {
	apiName := c.Param("name")

	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	pageToken := c.Query("page_token")

	overrides, nextToken, hasMore, err := h.overrideRepo.List(c.Request.Context(), apiName, limit, pageToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch overrides"})
		return
	}

	resp := model.OverridePage{Data: overrides, HasMore: hasMore}
	if hasMore {
		resp.NextPageToken = nextToken
	}
	c.JSON(http.StatusOK, resp)
}

// CreateOverride inserts (or upserts) a wallet override in ScyllaDB.
func (h *APIHandler) CreateOverride(c *gin.Context) {
	apiName := c.Param("name")

	var o model.Override
	if err := c.ShouldBindJSON(&o); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if o.Wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet is required"})
		return
	}

	if err := h.overrideRepo.Create(c.Request.Context(), apiName, o); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create override"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "ok"})
}

// DeleteOverride removes a wallet override from ScyllaDB.
func (h *APIHandler) DeleteOverride(c *gin.Context) {
	apiName := c.Param("name")
	wallet := c.Param("wallet")

	if err := h.overrideRepo.Delete(c.Request.Context(), apiName, wallet); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete override"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetWalletConfig returns the resolved rate-limit config for a specific wallet+API pair.
// Uses a single Redis MGET to fetch tier config and override in one round trip.
// Override absence is negatively cached to avoid ScyllaDB on every request.
func (h *APIHandler) GetWalletConfig(c *gin.Context) {
	apiName := c.Param("name")
	wallet := c.Param("wallet")

	resolved, err := h.tierRepo.GetWithOverride(c.Request.Context(), apiName, wallet)
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

package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shafikshaon/ratelimit/internal/model"
	"github.com/shafikshaon/ratelimit/internal/repository"
)

type APIHandler struct {
	repo *repository.APIRepository
}

func NewAPIHandler(repo *repository.APIRepository) *APIHandler {
	return &APIHandler{repo: repo}
}

func (h *APIHandler) ListAPIs(c *gin.Context) {
	groups, err := h.repo.ListGrouped(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch APIs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": groups})
}

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

	if err := h.repo.UpdateTier(c.Request.Context(), apiName, tierNum, tier); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update tier"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

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

	if err := h.repo.CreateOverride(c.Request.Context(), apiName, o); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "wallet override already exists"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "ok"})
}

func (h *APIHandler) DeleteOverride(c *gin.Context) {
	apiName := c.Param("name")
	wallet := c.Param("wallet")

	if err := h.repo.DeleteOverride(c.Request.Context(), apiName, wallet); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete override"})
		return
	}
	c.Status(http.StatusNoContent)
}

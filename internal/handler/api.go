package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

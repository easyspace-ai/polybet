package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) handleConnectivitySnapshot(c *gin.Context) {
	if h.conn == nil {
		c.JSON(http.StatusOK, gin.H{"dashConnected": false})
		return
	}
	c.JSON(http.StatusOK, h.conn.LegacyWSStatus())
}

func (h *Handler) handleWSStatus(c *gin.Context) {
	// Deprecated alias: O(1) registry read only.
	h.handleConnectivitySnapshot(c)
}

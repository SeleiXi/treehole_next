package utils

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const RequestIDHeader = "X-Treehole-Request-ID"

func EnsureRequestID(c *fiber.Ctx, requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	if c != nil {
		c.Set(RequestIDHeader, requestID)
		c.Set("X-Request-ID", requestID)
	}
	return requestID
}

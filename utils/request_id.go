package utils

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const RequestIDHeader = "X-Treehole-Request-ID"
const RequestIDQuery = "request_id"

func IncomingRequestID(c *fiber.Ctx, requestIDs ...string) string {
	for _, requestID := range requestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID != "" {
			return requestID
		}
	}
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Query(RequestIDQuery))
}

func EnsureRequestID(c *fiber.Ctx, requestID string) string {
	requestID = IncomingRequestID(c, requestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	if c != nil {
		c.Set(RequestIDHeader, requestID)
		c.Set("X-Request-ID", requestID)
	}
	return requestID
}

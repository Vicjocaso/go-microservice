package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// Server represents a server client in the system
type Server struct {
	ID        string `json:"id"`
	HashedPIN string `json:"-"`    // Don't expose PIN hash
	Role      string `json:"role"` // e.g., "calculator-server", "data-processor"
}

// In-memory store for demo purposes. In production, use a secure database.
var servers = map[string]*Server{
	"calculator-server": {ID: "calculator-server", HashedPIN: hashPIN("123"), Role: "calculator"},
	"analytics-server":  {ID: "analytics-server", HashedPIN: hashPIN("456"), Role: "analytics"},
	"admin-server":      {ID: "admin-server", HashedPIN: hashPIN("789"), Role: "admin"},
}

func hashPIN(pin string) string {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		panic(err) // In production, handle this gracefully
	}
	return string(bytes)
}

func checkPINHash(pin, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin))
	return err == nil
}

// ServerAuthMiddleware provides API key/PIN based authentication for server-to-server communication.
// It expects X-Server-ID and X-PIN headers.
func ServerAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := strings.TrimSuffix(c.Request().URL.Path, "/")
			if path == "/health" {
				return next(c)
			}

			serverID := c.Request().Header.Get("X-Server-ID")
			pin := c.Request().Header.Get("X-PIN")

			if serverID == "" || pin == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "Missing X-Server-ID or X-PIN headers")
			}

			server, ok := servers[serverID]
			if !ok || !checkPINHash(pin, server.HashedPIN) {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid Server ID or PIN")
			}

			// Store server ID and role in context for later use in handlers
			c.Set("serverID", server.ID)
			c.Set("serverRole", server.Role)

			return next(c)
		}
	}
}

// RBACMiddleware checks server roles for authorization.
func RBACMiddleware(requiredRole string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			serverRole, ok := c.Get("serverRole").(string)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "Server role not found in context")
			}

			if serverRole != requiredRole && serverRole != "admin" { // Admin server can do anything
				return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("Access denied. Requires role: %s", requiredRole))
			}
			return next(c)
		}
	}
}

// GetServerIDFromContext extracts the authenticated server ID from Echo context.
func GetServerIDFromContext(c echo.Context) (string, error) {
	serverID, ok := c.Get("serverID").(string)
	if !ok {
		return "", fmt.Errorf("server ID not found in context")
	}
	return serverID, nil
}

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const cslHourlyAuthScheme = "CslHourly "

// CslHourlyAuth validates requests to the csl_hourly query endpoints using a
// long-lived HMAC-SHA256 signature. No session or access token is required.
//
// The caller provides:
//
//	Authorization: CslHourly <hex(HMAC-SHA256(CSL_HOURLY_QUERY_KEY, username+":"+group_name))>
//
// The signature is stable: it does not change as long as CSL_HOURLY_QUERY_KEY,
// username, and group_name remain unchanged. To revoke access, rotate the key.
//
// On success the following values are set in the gin context:
//
//	"id"         int
//	"username"   string
//	"role"       int
//	"group_name" string
func CslHourlyAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		queryKey := os.Getenv("CSL_HOURLY_QUERY_KEY")
		if queryKey == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "csl_hourly query is not enabled",
			})
			c.Abort()
			return
		}

		authHeader := c.Request.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, cslHourlyAuthScheme) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid Authorization header",
			})
			c.Abort()
			return
		}
		providedSig := strings.TrimPrefix(authHeader, cslHourlyAuthScheme)

		username := c.Query("username")
		groupName := c.Query("group_name")
		if username == "" || groupName == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "username and group_name are required",
			})
			c.Abort()
			return
		}

		// Verify user exists and belongs to the claimed group.
		var user model.User
		if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "invalid credentials: no such user",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "database error",
				})
			}
			c.Abort()
			return
		}
		if user.Group != groupName {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid credentials: unmatched group_name",
			})
			c.Abort()
			return
		}

		// Verify HMAC-SHA256 signature.
		mac := hmac.New(sha256.New, []byte(queryKey))
		mac.Write([]byte(username + ":" + groupName))
		expectedSig := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid credentials: query signature does not verify",
			})
			c.Abort()
			return
		}

		c.Set("id", user.Id)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("group_name", user.Group)
		c.Next()
	}
}

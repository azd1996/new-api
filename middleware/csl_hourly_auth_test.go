package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCslHourlyAuthTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func signCslHourlyForTest(key, username, groupName string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(username + ":" + groupName))
	return hex.EncodeToString(mac.Sum(nil))
}

func performCslHourlyAuthRequest(t *testing.T, target string, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.GET("/api/log/hourly", CslHourlyAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"id":         c.GetInt("id"),
			"username":   c.GetString("username"),
			"role":       c.GetInt("role"),
			"group_name": c.GetString("group_name"),
		})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestCslHourlyAuthRejectsMissingQueryKey(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice&group_name=vip", "CslHourly ignored")

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestCslHourlyAuthRejectsInvalidAuthorizationHeader(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice&group_name=vip", "")

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestCslHourlyAuthRejectsMissingUsernameOrGroupName(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice", "CslHourly abc")

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestCslHourlyAuthRejectsUnknownUser(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")
	sig := signCslHourlyForTest("secret", "missing", "vip")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=missing&group_name=vip", "CslHourly "+sig)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestCslHourlyAuthRejectsGroupMismatch(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")
	require.NoError(t, model.DB.Create(&model.User{Username: "alice", Group: "default", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	sig := signCslHourlyForTest("secret", "alice", "vip")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice&group_name=vip", "CslHourly "+sig)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestCslHourlyAuthRejectsBadSignature(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")
	require.NoError(t, model.DB.Create(&model.User{Username: "alice", Group: "vip", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	sig := signCslHourlyForTest("wrong", "alice", "vip")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice&group_name=vip", "CslHourly "+sig)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestCslHourlyAuthAcceptsValidSignature(t *testing.T) {
	setupCslHourlyAuthTestDB(t)
	t.Setenv("CSL_HOURLY_QUERY_KEY", "secret")
	require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "alice", Group: "vip", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	sig := signCslHourlyForTest("secret", "alice", "vip")

	recorder := performCslHourlyAuthRequest(t, "/api/log/hourly?username=alice&group_name=vip", "CslHourly "+sig)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"username":"alice"`)
	assert.Contains(t, recorder.Body.String(), `"group_name":"vip"`)
	assert.Contains(t, recorder.Body.String(), `"role":10`)
}

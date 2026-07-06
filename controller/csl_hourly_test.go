package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cslHourlyResponse struct {
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	Data       []model.CslHourly `json:"data"`
	QueryStart int64             `json:"query_start"`
	QueryEnd   int64             `json:"query_end"`
}

func setupCslHourlyControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CslHourly{}))
	require.NoError(t, model.LOG_DB.Create(&model.CslHourly{
		StartTime: 3600,
		EndTime:   7200,
		Username:  "alice",
		GroupName: "vip",
		ModelName: "gpt-a",
		TokenName: "primary",
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.CslHourly{
		StartTime: 3600,
		EndTime:   7200,
		Username:  "bob",
		GroupName: "vip",
		ModelName: "gpt-b",
		TokenName: "backup",
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.CslHourly{
		StartTime: 3600,
		EndTime:   7200,
		Username:  "carol",
		GroupName: "default",
		ModelName: "gpt-c",
		TokenName: "other",
	}).Error)
}

func decodeCslHourlyResponse(t *testing.T, recorder *httptest.ResponseRecorder) cslHourlyResponse {
	t.Helper()
	t.Logf("csl hourly response: status=%d body=%s", recorder.Code, recorder.Body.String())
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload cslHourlyResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestGetUserCslHourlyRejectsInvalidStartTimestamp(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start_timestamp=0&end_timestamp=7200", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "invalid start_timestamp", payload.Message)
}

func TestGetUserCslHourlyRejectsInvalidEndTimestamp(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start_timestamp=3600&end_timestamp=0", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "invalid end_timestamp", payload.Message)
}

func TestGetUserCslHourlyRejectsReversedTimeRange(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start_timestamp=7200&end_timestamp=3600", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "invalid time range", payload.Message)
}

func TestGetUserCslHourlyRejectsTooWideTimeRange(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start_timestamp=3600&end_timestamp=612001", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "time range must not exceed 7 days", payload.Message)
}

func TestGetAllCslHourlyRejectsNonAdminRole(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleCommonUser)
	ctx.Set("group_name", "vip")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/hourly?start_timestamp=3600&end_timestamp=7200", nil)

	GetAllCslHourly(ctx)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestGetAllCslHourlyRestrictsToAuthenticatedGroup(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("group_name", "vip")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/hourly?start_timestamp=3601&end_timestamp=7201", nil)

	GetAllCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, int64(3600), payload.QueryStart)
	assert.Equal(t, int64(7200), payload.QueryEnd)
	require.Len(t, payload.Data, 2)
	for _, row := range payload.Data {
		assert.Equal(t, "vip", row.GroupName)
	}
}

func TestGetUserCslHourlyRestrictsToAuthenticatedUsernameAndToken(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start_timestamp=3601&end_timestamp=7201&token_name=primary", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, int64(3600), payload.QueryStart)
	assert.Equal(t, int64(7200), payload.QueryEnd)
	require.Len(t, payload.Data, 1)
	assert.Equal(t, "alice", payload.Data[0].Username)
	assert.Equal(t, "primary", payload.Data[0].TokenName)
	assert.Equal(t, int64(3600), payload.Data[0].StartTime)
}

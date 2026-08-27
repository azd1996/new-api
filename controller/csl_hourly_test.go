package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		StartTime:       3600,
		EndTime:         7200,
		Username:        "alice",
		GroupName:       "vip",
		ModelName:       "gpt-a",
		TokenName:       "primary",
		ChannelId:       17,
		ChannelDiscount: 0.85,
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

func TestParseCslHourlyHourRange(t *testing.T) {
	hourStart := func(t *testing.T, value string) int64 {
		t.Helper()
		parsed, err := time.ParseInLocation("2006-01-02-15", value, time.Local)
		require.NoError(t, err)
		return parsed.Unix()
	}

	cases := []struct {
		name         string
		query        string
		expectStart  int64
		expectEnd    int64
		expectErrMsg string
	}{
		{
			name:        "end defaults to start and covers the whole hour",
			query:       "start=2026-08-27-17",
			expectStart: hourStart(t, "2026-08-27-17"),
			expectEnd:   hourStart(t, "2026-08-27-17") + 3599,
		},
		{
			name:        "explicit end covers the whole end hour",
			query:       "start=2026-08-27-17&end=2026-08-28-09",
			expectStart: hourStart(t, "2026-08-27-17"),
			expectEnd:   hourStart(t, "2026-08-28-09") + 3599,
		},
		{
			name:        "compact layout is accepted",
			query:       "start=2026-0827-17&end=2026-0827-18",
			expectStart: hourStart(t, "2026-08-27-17"),
			expectEnd:   hourStart(t, "2026-08-27-18") + 3599,
		},
		{
			name:        "start and end win over start_timestamp and end_timestamp",
			query:       "start=2026-08-27-17&start_timestamp=3600&end_timestamp=7200",
			expectStart: hourStart(t, "2026-08-27-17"),
			expectEnd:   hourStart(t, "2026-08-27-17") + 3599,
		},
		{
			name:         "malformed start reports the expected format",
			query:        "start=2026%2F08%2F27+17%3A30",
			expectErrMsg: `invalid start, it must be an hour like "2026-08-27-17" (YYYY-MM-DD-HH in server local time)`,
		},
		{
			name:         "end without start reports the expected format for start",
			query:        "end=2026-08-27-17",
			expectErrMsg: `invalid start, it must be an hour like "2026-08-27-17" (YYYY-MM-DD-HH in server local time)`,
		},
		{
			name:         "malformed end reports the expected format",
			query:        "start=2026-08-27-17&end=2026-08-27",
			expectErrMsg: `invalid end, it must be an hour like "2026-08-27-17" (YYYY-MM-DD-HH in server local time)`,
		},
		{
			name:         "reversed hour range is rejected",
			query:        "start=2026-08-27-17&end=2026-08-27-16",
			expectErrMsg: "invalid time range",
		},
		{
			name:         "hour range wider than 7 days is rejected",
			query:        "start=2026-08-20-17&end=2026-08-27-18",
			expectErrMsg: "time range must not exceed 7 days",
		},
		{
			name:        "hour range of exactly 7 days is accepted",
			query:       "start=2026-08-20-17&end=2026-08-27-17",
			expectStart: hourStart(t, "2026-08-20-17"),
			expectEnd:   hourStart(t, "2026-08-27-17") + 3599,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?"+testCase.query, nil)

			startTimestamp, endTimestamp, ok := parseCslHourlyTimeRange(ctx)

			if testCase.expectErrMsg != "" {
				require.False(t, ok)
				var payload cslHourlyResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
				assert.False(t, payload.Success)
				assert.Equal(t, testCase.expectErrMsg, payload.Message)
				return
			}
			require.True(t, ok, recorder.Body.String())
			assert.Equal(t, testCase.expectStart, startTimestamp)
			assert.Equal(t, testCase.expectEnd, endTimestamp)
		})
	}
}

func TestGetUserCslHourlyAcceptsHourGranularityRange(t *testing.T) {
	setupCslHourlyControllerTestDB(t)

	hour := time.Date(2026, 8, 27, 17, 0, 0, 0, time.Local)
	require.NoError(t, model.LOG_DB.Create(&model.CslHourly{
		StartTime: hour.Unix(),
		EndTime:   hour.Unix() + 3600,
		Username:  "alice",
		GroupName: "vip",
		ModelName: "gpt-a",
		TokenName: "primary",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/hourly?start=2026-08-27-17", nil)

	GetUserCslHourly(ctx)

	payload := decodeCslHourlyResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, hour.Unix(), payload.QueryStart)
	assert.Equal(t, hour.Unix()+3599, payload.QueryEnd)
	require.Len(t, payload.Data, 1)
	assert.Equal(t, hour.Unix(), payload.Data[0].StartTime)
	assert.Equal(t, "alice", payload.Data[0].Username)
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
	assert.Equal(t, 17, payload.Data[0].ChannelId)
	assert.InDelta(t, 0.85, payload.Data[0].ChannelDiscount, 0.000001)
}

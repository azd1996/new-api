package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNaturalQuarterHourSlotUsesPreviousCompletedWindow(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 15, 8, 0, time.FixedZone("CST", 8*60*60))

	start, end := NaturalQuarterHourSlot(now)

	require.Equal(t, time.Date(2026, 11, 6, 12, 0, 0, 0, now.Location()), start)
	require.Equal(t, time.Date(2026, 11, 6, 12, 15, 0, 0, now.Location()), end)
	require.Equal(t, "error_alert:"+strconv.FormatInt(end.Unix(), 10), ErrorAlertScheduleKey(end))
}

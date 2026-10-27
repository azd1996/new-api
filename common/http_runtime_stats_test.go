package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRequestRateWindowCountsOnlyLast60Seconds(t *testing.T) {
	var window requestRateWindow
	start := time.Unix(1_800_000_000, 0)
	window.record(start)
	window.record(start.Add(59 * time.Second))

	assert.Equal(t, int64(2), window.count(start.Add(59*time.Second)))

	window.record(start.Add(60 * time.Second))
	assert.Equal(t, int64(2), window.count(start.Add(60*time.Second)))
	assert.Equal(t, int64(1), window.count(start.Add(119*time.Second)))
}

package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
)

// TestSessionOtherLikeConditionEscapes locks the cross-dialect LIKE building so
// a session id carrying LIKE metacharacters (%, _, escape char) cannot alter
// the search pattern.
func TestSessionOtherLikeConditionEscapes(t *testing.T) {
	orig := common.LogDatabaseType()
	defer common.SetLogDatabaseType(orig)

	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	cond, pat := sessionOtherLikeCondition(`"session_id":"a%b_c!d"`)
	assert.Equal(t, "other LIKE ? ESCAPE '!'", cond)
	assert.Equal(t, `%"session!_id":"a!%b!_c!!d"%`, pat)

	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	cond, pat = sessionOtherLikeCondition(`"session_id":"a%b_c\d"`)
	assert.Equal(t, "other LIKE ?", cond)
	assert.Equal(t, `%"session\_id":"a\%b\_c\\d"%`, pat)
}

package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestParseMySQLVersion(t *testing.T) {
	cases := []struct {
		in                  string
		major, minor, patch int
	}{
		{"8.0.36", 8, 0, 36},
		{"5.6.51-log", 5, 6, 51},
		{"5.7.44-0ubuntu0.18.04.1", 5, 7, 44},
		{"5.7.8", 5, 7, 8},
		{"8.4.0", 8, 4, 0},
		{"", 0, 0, 0},
	}
	for _, c := range cases {
		ma, mi, pa := parseMySQLVersion(c.in)
		assert.Equalf(t, c.major, ma, "major of %q", c.in)
		assert.Equalf(t, c.minor, mi, "minor of %q", c.in)
		assert.Equalf(t, c.patch, pa, "patch of %q", c.in)
	}
}

func TestMySQLVersionSupportsJSON(t *testing.T) {
	cases := []struct {
		major, minor, patch int
		want                bool
	}{
		{5, 6, 51, false}, // MySQL 5.6: no native JSON
		{5, 7, 7, false},  // just before JSON was introduced
		{5, 7, 8, true},   // JSON introduced in 5.7.8
		{5, 7, 44, true},
		{8, 0, 36, true},
		{8, 4, 0, true},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, mysqlVersionSupportsJSON(c.major, c.minor, c.patch),
			"%d.%d.%d", c.major, c.minor, c.patch)
	}
}

func TestJSONColumnType(t *testing.T) {
	orig := mysqlSupportsNativeJSON
	t.Cleanup(func() { mysqlSupportsNativeJSON = orig })

	mysqlDB := &gorm.DB{Config: &gorm.Config{Dialector: mysql.New(mysql.Config{})}}
	pgDB := &gorm.DB{Config: &gorm.Config{Dialector: postgres.New(postgres.Config{})}}
	sqliteDB := &gorm.DB{Config: &gorm.Config{Dialector: sqlite.Open(":memory:")}}

	// When the server supports native JSON (also the default for PG/SQLite), every
	// dialect keeps the "json" column type so existing deployments see no DDL change.
	mysqlSupportsNativeJSON = true
	assert.Equal(t, "json", jsonColumnType(mysqlDB))
	assert.Equal(t, "json", jsonColumnType(pgDB))
	assert.Equal(t, "json", jsonColumnType(sqliteDB))

	// Only a confirmed pre-5.7.8 MySQL server downgrades to longtext; PostgreSQL and
	// SQLite are unaffected.
	mysqlSupportsNativeJSON = false
	assert.Equal(t, "longtext", jsonColumnType(mysqlDB))
	assert.Equal(t, "json", jsonColumnType(pgDB))
	assert.Equal(t, "json", jsonColumnType(sqliteDB))
}

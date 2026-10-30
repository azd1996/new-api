package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// mysqlSupportsNativeJSON reports whether the connected MySQL server supports the
// native JSON column type (MySQL >= 5.7.8). It defaults to true so that PostgreSQL,
// SQLite, MariaDB, and any failed/unknown detection keep using the JSON type exactly
// as before; only a confirmed pre-5.7.8 MySQL server flips it to false.
var mysqlSupportsNativeJSON = true

// detectMySQLJSONSupport probes the MySQL server version once at startup and records
// whether the native JSON column type is available. It must be called only after the
// connection has already been verified (right after checkMySQLChineseSupport), so a
// query failure here is abnormal rather than a "database not ready" condition; in that
// case we keep the default (assume JSON support) and log, letting a genuinely
// unsupported server surface a clear error during AutoMigrate instead.
func detectMySQLJSONSupport(db *gorm.DB) {
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil || version == "" {
		common.SysError(fmt.Sprintf("failed to detect MySQL version (%v), assuming native JSON support", err))
		return
	}
	// MariaDB exposes JSON as a LONGTEXT alias since 10.2 and historically prefixes its
	// version string with "5.5.5-"; treat it as JSON-capable and skip numeric parsing.
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return
	}
	major, minor, patch := parseMySQLVersion(version)
	mysqlSupportsNativeJSON = mysqlVersionSupportsJSON(major, minor, patch)
	if !mysqlSupportsNativeJSON {
		common.SysLog(fmt.Sprintf("MySQL server %q lacks native JSON type; using longtext for JSON columns", version))
	}
}

// mysqlVersionSupportsJSON reports whether a MySQL server of the given version has
// the native JSON column type, which was introduced in 5.7.8.
func mysqlVersionSupportsJSON(major, minor, patch int) bool {
	return major > 5 ||
		(major == 5 && minor > 7) ||
		(major == 5 && minor == 7 && patch >= 8)
}

// parseMySQLVersion extracts the leading major.minor.patch numbers from a MySQL
// version string such as "8.0.36", "5.6.51-log", or "5.7.44-0ubuntu0.18.04.1".
func parseMySQLVersion(version string) (major, minor, patch int) {
	head := version
	if idx := strings.IndexAny(head, "-~ "); idx >= 0 {
		head = head[:idx]
	}
	parts := strings.Split(head, ".")
	at := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
		return n
	}
	return at(0), at(1), at(2)
}

// jsonColumnType returns the SQL column type used for JSON-bearing fields. Only a
// confirmed pre-5.7.8 MySQL server is downgraded to longtext (4GB, matching JSON
// capacity); every other case (MySQL >= 5.7.8, PostgreSQL, SQLite) keeps the native
// "json" type, so existing deployments observe no DDL change and no migration.
func jsonColumnType(db *gorm.DB) string {
	if db.Dialector.Name() == "mysql" && !mysqlSupportsNativeJSON {
		return "longtext"
	}
	return "json"
}

// GormDBDataType implementations route the JSON-bearing model fields through
// jsonColumnType so the stored column type follows the dialect/version. Defining them
// centrally keeps the JSON-storage policy in one place; removing the previous
// `gorm:"type:json"` tags is required because a struct tag would override these.
func (ChannelInfo) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonColumnType(db) }

func (Properties) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonColumnType(db) }

func (TaskPrivateData) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	return jsonColumnType(db)
}

func (JSONValue) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonColumnType(db) }

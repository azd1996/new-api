package model

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ErrorSignatureAlias maps a normalized error signature (by its hash) to an
// admin-chosen short name ("明细错误类型" / category) and error attribution
// ("错误归因"). Both are many-signatures-to-one-value; signatures without a row
// resolve to empty (rendered as "uncategorized" / "unattributed"). The short
// name is also exported as a bounded Prometheus label, keeping metric
// cardinality under admin control even though signatures are high-cardinality.
type ErrorSignatureAlias struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	SignatureHash string `json:"signature_hash" gorm:"type:varchar(32);uniqueIndex"`
	Signature     string `json:"signature" gorm:"type:text"`
	ShortName     string `json:"short_name" gorm:"type:varchar(128);index;default:''"`
	Attribution   string `json:"attribution" gorm:"type:varchar(128);index;default:''"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

// errorAliasEntry is the cached resolution for one signature hash.
type errorAliasEntry struct {
	ShortName   string
	Attribution string
}

var (
	errorAliasCacheMu sync.RWMutex
	errorAliasCache   = map[string]errorAliasEntry{}
)

// GetAllErrorSignatureAliases returns every configured signature→short-name
// mapping, ordered by short name for stable admin display.
func GetAllErrorSignatureAliases() ([]*ErrorSignatureAlias, error) {
	var aliases []*ErrorSignatureAlias
	err := DB.Order("short_name asc").Find(&aliases).Error
	return aliases, err
}

// UpsertErrorSignatureAlias sets (or replaces) the short name + attribution for
// a signature hash and refreshes the in-memory cache. It uses find-then-save/
// create instead of dialect-specific upsert so it works on SQLite, MySQL and
// PostgreSQL alike.
func UpsertErrorSignatureAlias(hash string, signature string, shortName string, attribution string) error {
	if hash == "" {
		return errors.New("signature hash is required")
	}
	now := common.GetTimestamp()
	var existing ErrorSignatureAlias
	err := DB.Where("signature_hash = ?", hash).First(&existing).Error
	switch {
	case err == nil:
		existing.Signature = signature
		existing.ShortName = shortName
		existing.Attribution = attribution
		existing.UpdatedAt = now
		if err := DB.Save(&existing).Error; err != nil {
			return err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		rec := ErrorSignatureAlias{
			SignatureHash: hash,
			Signature:     signature,
			ShortName:     shortName,
			Attribution:   attribution,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := DB.Create(&rec).Error; err != nil {
			return err
		}
	default:
		return err
	}
	return ReloadErrorAliasCache()
}

// DeleteErrorSignatureAlias removes a mapping (the signature reverts to
// "uncategorized") and refreshes the cache.
func DeleteErrorSignatureAlias(hash string) error {
	if hash == "" {
		return errors.New("signature hash is required")
	}
	if err := DB.Where("signature_hash = ?", hash).Delete(&ErrorSignatureAlias{}).Error; err != nil {
		return err
	}
	return ReloadErrorAliasCache()
}

// ReloadErrorAliasCache rebuilds the hash→{short-name,attribution} cache.
func ReloadErrorAliasCache() error {
	aliases, err := GetAllErrorSignatureAliases()
	if err != nil {
		return err
	}
	m := make(map[string]errorAliasEntry, len(aliases))
	for _, a := range aliases {
		m[a.SignatureHash] = errorAliasEntry{ShortName: a.ShortName, Attribution: a.Attribution}
	}
	errorAliasCacheMu.Lock()
	errorAliasCache = m
	errorAliasCacheMu.Unlock()
	return nil
}

// InitErrorAliasCache loads the cache at startup. Failures are logged but not
// fatal; an empty cache just means every signature resolves to uncategorized.
func InitErrorAliasCache() {
	if err := ReloadErrorAliasCache(); err != nil {
		common.SysError("failed to load error signature alias cache: " + err.Error())
	}
}

// ResolveErrorShortName returns the configured short name for a signature hash,
// or "" when none is configured.
func ResolveErrorShortName(hash string) string {
	if hash == "" {
		return ""
	}
	errorAliasCacheMu.RLock()
	defer errorAliasCacheMu.RUnlock()
	return errorAliasCache[hash].ShortName
}

// ResolveErrorAttribution returns the configured attribution for a signature
// hash, or "" when none is configured.
func ResolveErrorAttribution(hash string) string {
	if hash == "" {
		return ""
	}
	errorAliasCacheMu.RLock()
	defer errorAliasCacheMu.RUnlock()
	return errorAliasCache[hash].Attribution
}

// SyncErrorAliasCache periodically rebuilds the alias cache from the DB so
// short-name mappings created/edited on another instance propagate to this one
// without a restart, mirroring SyncChannelCache / SyncOptions. frequency is the
// SYNC_FREQUENCY interval in seconds; a non-positive value disables polling.
func SyncErrorAliasCache(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		if err := ReloadErrorAliasCache(); err != nil {
			common.SysError("failed to sync error signature alias cache: " + err.Error())
		}
	}
}

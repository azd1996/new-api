package model

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ErrorSignatureAlias maps a normalized error signature (by its hash) to an
// admin-chosen short name ("明细错误类型" / category). The mapping is
// many-signatures-to-one-shortname; signatures without a row resolve to the
// empty short name, which callers render as "uncategorized". The short name is
// what gets exported as a bounded Prometheus label, keeping metric cardinality
// under admin control even though signatures themselves are high-cardinality.
type ErrorSignatureAlias struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	SignatureHash string `json:"signature_hash" gorm:"type:varchar(32);uniqueIndex"`
	Signature     string `json:"signature" gorm:"type:text"`
	ShortName     string `json:"short_name" gorm:"type:varchar(128);index;default:''"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

var (
	errorAliasCacheMu sync.RWMutex
	errorAliasCache   = map[string]string{}
)

// GetAllErrorSignatureAliases returns every configured signature→short-name
// mapping, ordered by short name for stable admin display.
func GetAllErrorSignatureAliases() ([]*ErrorSignatureAlias, error) {
	var aliases []*ErrorSignatureAlias
	err := DB.Order("short_name asc").Find(&aliases).Error
	return aliases, err
}

// UpsertErrorSignatureAlias sets (or replaces) the short name for a signature
// hash and refreshes the in-memory cache. It uses find-then-save/create instead
// of dialect-specific upsert so it works on SQLite, MySQL and PostgreSQL alike.
func UpsertErrorSignatureAlias(hash string, signature string, shortName string) error {
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
		existing.UpdatedAt = now
		if err := DB.Save(&existing).Error; err != nil {
			return err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		rec := ErrorSignatureAlias{
			SignatureHash: hash,
			Signature:     signature,
			ShortName:     shortName,
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

// ReloadErrorAliasCache rebuilds the hash→short-name cache from the database.
func ReloadErrorAliasCache() error {
	aliases, err := GetAllErrorSignatureAliases()
	if err != nil {
		return err
	}
	m := make(map[string]string, len(aliases))
	for _, a := range aliases {
		m[a.SignatureHash] = a.ShortName
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
	return errorAliasCache[hash]
}

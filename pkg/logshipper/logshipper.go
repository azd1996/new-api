// Package logshipper writes new-api log rows to a local rotated file so that
// LoongCollector can ship them to the high-availability ClickHouse cluster via
// Kafka, without new-api itself connecting to the cluster.
//
// This is a dual-write path, not a replacement: rows keep going into LOG_DB
// (the single-node ClickHouse or MySQL) exactly as before, because the admin
// log UI and csl-sidecar still read them via SQL. The two copies also serve as
// a reconciliation baseline while the new pipeline is being validated.
//
// The underlying writer comes from ai-gateway/csl-logshipper, synced into
// third_party/ at build time (see ai-gateway/newapi/docker/sync-csl-logshipper.sh).
package logshipper

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/third_party/csl-logshipper/writer"
)

// SchemaNewAPILogsV1 tags every line written by Ship. LoongCollector's
// processor config uses it to route these lines to the raw-logs table; it is
// unrelated to any OmniDataSearch metric name used to query them back.
const SchemaNewAPILogsV1 = "newapi_logs_v1"

// Config mirrors csl-logshipper's writer.Config plus the instance identity.
// Every field is settable through the LOG_SHIPPER_* environment variables
// (see common.InitEnv).
type Config struct {
	// Filename is the local log file path. Required.
	Filename string

	// InstanceName is emitted as x_instance_name so rows from different
	// new-api instances stay distinguishable in the shared cluster table.
	// Required: an empty value would silently merge them.
	InstanceName string

	// MaxSize, MaxBackups, MaxAge, LocalTime and Compress are forwarded to
	// writer.Config unchanged; zero values fall back to lumberjack defaults.
	MaxSize    int
	MaxBackups int
	MaxAge     int
	LocalTime  bool
	Compress   bool
}

// Row is the JSON shape written per log entry. Field tags are the column names
// of the *cluster* logs table, which are interchangeable with neither the json
// tags on model.Log nor the single-node ClickHouse columns:
//
//   - model.Log tags CreatedAt as `created_at`; the cluster table calls it
//     `ts` and derives its `dt` partition from it via
//     MATERIALIZED toYYYYMMDD(toDateTime(ts)). A mismatched key here would
//     leave ts at its DEFAULT 0 and pile every row into the 19700101
//     partition without any error.
//   - model.Log tags ChannelId as `channel`; both tables use `channel_id`.
//   - model.Log tags Group as `group` and the single-node column is the
//     reserved word `group`; the cluster table renamed it to `group_name`.
//   - model.Log.Id and model.Log.ChannelName have no cluster column at all.
//     Id is deliberately absent: it is a LOG_DB primary key with no meaning
//     in the cluster table, where a row is identified by
//     (x_instance_name, ts, request_id, type).
//
// LoongCollector -> Kafka -> Flink -> ClickHouse matches by column name, so a
// mismatched key silently lands as the column DEFAULT instead of failing.
//
// IsStream stays a JSON bool: the contract handed to the data team
// (ai-gateway/newapi/csl-sidecar/ck-ingest-fields.md) specifies a bool here
// and puts the UInt8 0/1 conversion on the ingest side. Emitting 0/1 from
// here would make their bool parse miss and turn every streamed request into
// a non-streamed one.
type Row struct {
	UserId            int    `json:"user_id"`
	Ts                int64  `json:"ts"`
	Type              int    `json:"type"`
	Content           string `json:"content"`
	Username          string `json:"username"`
	TokenName         string `json:"token_name"`
	ModelName         string `json:"model_name"`
	Quota             int    `json:"quota"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	UseTime           int    `json:"use_time"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel_id"`
	TokenId           int    `json:"token_id"`
	GroupName         string `json:"group_name"`
	Ip                string `json:"ip"`
	RequestId         string `json:"request_id"`
	UpstreamRequestId string `json:"upstream_request_id"`
	Other             string `json:"other"`

	// XInstanceName is the cluster table's leading ORDER BY column; several
	// instances share one table. Ship fills it in, callers leave it zero.
	XInstanceName string `json:"x_instance_name"`
}

var (
	mu           sync.RWMutex
	w            *writer.Writer
	instanceName string
)

// Init opens the log file. Calling it when already initialised is an error
// rather than a silent reopen, so a duplicated startup path is caught.
func Init(cfg Config) error {
	if cfg.Filename == "" {
		return fmt.Errorf("logshipper: Config.Filename must not be empty")
	}
	if cfg.InstanceName == "" {
		return fmt.Errorf("logshipper: Config.InstanceName must not be empty")
	}

	mu.Lock()
	defer mu.Unlock()
	if w != nil {
		return fmt.Errorf("logshipper: already initialised")
	}

	created, err := writer.New(writer.Config{
		Filename:   cfg.Filename,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		LocalTime:  cfg.LocalTime,
		Compress:   cfg.Compress,
	})
	if err != nil {
		return fmt.Errorf("logshipper: open %s: %w", cfg.Filename, err)
	}
	w = created
	instanceName = cfg.InstanceName
	return nil
}

// Enabled reports whether Init has run successfully.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return w != nil
}

// Ship writes one row, stamping x_instance_name. It returns an error instead
// of swallowing it so the caller decides how loud to be; callers on the relay
// hot path must not fail the request because of it.
//
// Ship is a no-op when the shipper is disabled, so callers do not need to
// guard on Enabled().
func Ship(row Row) error {
	mu.RLock()
	current, name := w, instanceName
	mu.RUnlock()

	if current == nil {
		return nil
	}
	row.XInstanceName = name
	return current.Write(SchemaNewAPILogsV1, row)
}

// Close releases the log file. Safe to call when Init never ran.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		return nil
	}
	err := w.Close()
	w = nil
	instanceName = ""
	return err
}

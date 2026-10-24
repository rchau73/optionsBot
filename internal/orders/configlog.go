package orders

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/config"
)

// ConfigRecord is the startup line that dates every change of the bot's
// settings (#256): the fingerprint of the effective strategy settings, the
// settings themselves (so the next start can say what changed) and the keys
// that changed since the previous config_loaded line. Nothing from .env is
// in it (config.Effective). Actor stays empty until settings are changed
// through an apply flow: today files are edited by hand.
type ConfigRecord struct {
	Timestamp   time.Time       `json:"timestamp"`
	Event       string          `json:"event"` // always "config_loaded"
	StrategyID  string          `json:"strategy_id,omitempty"`
	Fingerprint string          `json:"fingerprint"`
	Version     string          `json:"version,omitempty"`
	Deployment  string          `json:"deployment,omitempty"`
	ConfigFile  string          `json:"config_file,omitempty"`
	First       bool            `json:"first,omitempty"`     // no earlier config_loaded line in the journal
	Unchanged   bool            `json:"unchanged,omitempty"` // same fingerprint as the last one: a restart
	Changes     []config.Change `json:"changes"`
	// Since is when the settings last changed: this start unless Unchanged.
	Since    time.Time      `json:"since"`
	Settings map[string]any `json:"settings"`
	Actor    *Actor         `json:"actor,omitempty"`
}

// ParseConfigRecord decodes a config_loaded journal line (Replay.LastConfig);
// an empty line is no record.
func ParseConfigRecord(line []byte) (*ConfigRecord, error) {
	if len(line) == 0 {
		return nil, nil
	}
	var r ConfigRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, fmt.Errorf("decode config_loaded line: %w", err)
	}
	return &r, nil
}

// NextConfigRecord is the config_loaded line for a start at now with the
// effective settings, after prev (the journal's last such line, nil when it
// has none). Pure: main fills in the file, version and deployment.
func NextConfigRecord(prev *ConfigRecord, settings map[string]any, now time.Time) ConfigRecord {
	r := ConfigRecord{
		Timestamp: now, Event: EventConfig, Fingerprint: config.Fingerprint(settings),
		Changes: []config.Change{}, Since: now, Settings: settings,
	}
	switch {
	case prev == nil:
		r.First = true
	case prev.Fingerprint == r.Fingerprint:
		r.Unchanged = true
		r.Since = prev.Since
		if r.Since.IsZero() {
			r.Since = prev.Timestamp
		}
	default:
		r.Changes = config.Diff(prev.Settings, settings)
	}
	return r
}

// LogStartConfig journals this start's config_loaded line after the journal's
// last one (Replay.LastConfig) and returns it. An unreadable last line is
// treated as none, with a warning: the settings are then journaled whole.
func (l *Logger) LogStartConfig(last []byte, cfg *config.Config, file, deployment, version string, now time.Time) ConfigRecord {
	prev, err := ParseConfigRecord(last)
	if err != nil {
		slog.Warn("journal: last config_loaded line unreadable; settings journaled as a first record", "err", err)
		prev = nil
	}
	r := NextConfigRecord(prev, config.Effective(cfg), now)
	r.StrategyID, r.ConfigFile, r.Deployment, r.Version = cfg.StrategyID, file, deployment, version
	l.LogConfig(r)
	keys := make([]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		keys = append(keys, c.Key)
	}
	slog.Info("settings journaled", "fingerprint", r.Fingerprint, "first", r.First, "unchanged", r.Unchanged,
		"changed", keys, "since", r.Since, "config_file", file)
	return r
}

// LogConfig journals a config_loaded line.
func (l *Logger) LogConfig(r ConfigRecord) {
	r.Event = EventConfig
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}
	l.writeJSON(EventConfig, r, slog.Default().With("event", EventConfig, "fingerprint", r.Fingerprint))
}

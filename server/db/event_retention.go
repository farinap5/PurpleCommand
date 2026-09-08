package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"purpcmd/pkg/teamapi"
)

const (
	EventRetentionTierShort     = "short"
	EventRetentionTierStandard  = "standard"
	EventRetentionTierImportant = "important"
	EventRetentionTierArchive   = "archive"
)

const (
	eventRetentionShort     = 24 * time.Hour
	eventRetentionStandard  = 7 * 24 * time.Hour
	eventRetentionImportant = 30 * 24 * time.Hour
	eventRetentionArchive   = 90 * 24 * time.Hour
	maxRetentionSeconds     = int64(1<<63-1) / int64(time.Second)
)

// EventRetentionConfiguration describes the database-backed retention policy
// for one event type. A zero retention duration disables expiration for that
// event type. Event types without a configuration are also retained.
type EventRetentionConfiguration struct {
	EventType string
	Tier      string
	Retention time.Duration
	UpdatedAt time.Time
}

var defaultEventRetentionConfigurations = []EventRetentionConfiguration{
	{EventType: teamapi.EventListenerCreated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerUpdated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerStarting, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerStarted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerStopping, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerStopped, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventListenerFailed, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventSessionRegistered, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSessionCheckin, Tier: EventRetentionTierShort, Retention: eventRetentionShort},
	{EventType: teamapi.EventSessionDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSessionOutput, Tier: EventRetentionTierArchive, Retention: eventRetentionArchive},
	{EventType: teamapi.EventTaskCreated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventTaskDispatched, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventTaskCompleted, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventLootCreated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventLootDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventScriptLoaded, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventScriptUnloaded, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventScriptOutput, Tier: EventRetentionTierArchive, Retention: eventRetentionArchive},
	{EventType: teamapi.EventProfileCreated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventProfileUpdated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventProfileDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventPayloadBuilderRegistered, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventPayloadBuilderUnregistered, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventBuildQueued, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventBuildStarted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventBuildOutput, Tier: EventRetentionTierArchive, Retention: eventRetentionArchive},
	{EventType: teamapi.EventBuildCompleted, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventBuildFailed, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventBuildDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventUserLogin, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventUserLogout, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventUserCreated, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventUserUpdated, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventUserDeleted, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventUserMessage, Tier: EventRetentionTierArchive, Retention: eventRetentionArchive},
	{EventType: teamapi.EventSpeakerCreated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerUpdated, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerConnecting, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerConnected, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerDisconnected, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerFailed, Tier: EventRetentionTierImportant, Retention: eventRetentionImportant},
	{EventType: teamapi.EventSpeakerStopped, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
	{EventType: teamapi.EventSpeakerDeleted, Tier: EventRetentionTierStandard, Retention: eventRetentionStandard},
}

func ensureDefaultEventRetentionConfigurations() error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	transaction, err := DBMS.DBConn.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, configuration := range defaultEventRetentionConfigurations {
		if _, err := transaction.Exec(`
			INSERT OR IGNORE INTO EventRetentionConfiguration
				(EventType, RetentionTier, RetentionSeconds, UpdatedAt)
			VALUES (?, ?, ?, ?);
		`, configuration.EventType, configuration.Tier, int64(configuration.Retention/time.Second), now); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func DBEventRetentionList() ([]EventRetentionConfiguration, error) {
	rows, err := DBMS.DBConn.Query(`
		SELECT EventType, RetentionTier, RetentionSeconds, UpdatedAt
		FROM EventRetentionConfiguration
		ORDER BY EventType;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configurations := make([]EventRetentionConfiguration, 0)
	for rows.Next() {
		var configuration EventRetentionConfiguration
		var retentionSeconds int64
		var updatedAt string
		if err := rows.Scan(&configuration.EventType, &configuration.Tier, &retentionSeconds, &updatedAt); err != nil {
			return nil, err
		}
		if retentionSeconds < 0 || retentionSeconds > maxRetentionSeconds {
			return nil, fmt.Errorf("invalid retention duration for event type %q", configuration.EventType)
		}
		configuration.Retention = time.Duration(retentionSeconds) * time.Second
		configuration.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse event retention timestamp for %q: %w", configuration.EventType, err)
		}
		configurations = append(configurations, configuration)
	}
	return configurations, rows.Err()
}

// DBEventRetentionSet creates or replaces the retention policy for an event
// type. A zero retention duration keeps matching events indefinitely.
func DBEventRetentionSet(eventType, tier string, retention time.Duration) error {
	eventType = strings.TrimSpace(eventType)
	tier = strings.TrimSpace(tier)
	if eventType == "" {
		return errors.New("event type is required")
	}
	if tier == "" {
		return errors.New("event retention tier is required")
	}
	if retention < 0 {
		return errors.New("event retention duration must not be negative")
	}
	if retention%time.Second != 0 {
		return errors.New("event retention duration must use whole seconds")
	}
	_, err := DBMS.DBConn.Exec(`
		INSERT INTO EventRetentionConfiguration
			(EventType, RetentionTier, RetentionSeconds, UpdatedAt)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(EventType) DO UPDATE SET
			RetentionTier = excluded.RetentionTier,
			RetentionSeconds = excluded.RetentionSeconds,
			UpdatedAt = excluded.UpdatedAt;
	`, eventType, tier, int64(retention/time.Second), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// DBEventPruneExpired removes all expired rows in batches. Each batch contains
// at most batchSize rows for one event type, which keeps individual writer
// locks and WAL transactions bounded while still allowing cleanup to catch up
// after a long period of downtime. Unconfigured event types and policies with
// a zero retention duration are left untouched.
func DBEventPruneExpired(now time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		return 0, errors.New("event retention batch size must be positive")
	}
	configurations, err := DBEventRetentionList()
	if err != nil {
		return 0, err
	}
	var deleted int64
	for _, configuration := range configurations {
		if configuration.Retention == 0 {
			continue
		}
		cutoff := now.UTC().Add(-configuration.Retention).Format(time.RFC3339Nano)
		for {
			result, err := DBMS.DBConn.Exec(`
				DELETE FROM Events
				WHERE Sequence IN (
					SELECT Sequence
					FROM Events
					WHERE Type = ? AND CreatedAt < ?
					ORDER BY CreatedAt, Sequence
					LIMIT ?
				);
			`, configuration.EventType, cutoff, batchSize)
			if err != nil {
				return deleted, fmt.Errorf("prune expired %s events: %w", configuration.EventType, err)
			}
			count, err := result.RowsAffected()
			if err != nil {
				return deleted, fmt.Errorf("count pruned %s events: %w", configuration.EventType, err)
			}
			deleted += count
			if count < int64(batchSize) {
				break
			}
		}
	}
	return deleted, nil
}

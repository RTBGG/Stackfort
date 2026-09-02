// SPDX-License-Identifier: AGPL-3.0-or-later

package updateapply

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	journalDigestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	journalErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)
)

func validateJournal(journal Journal) error {
	if journal.SchemaVersion != JournalSchemaVersion {
		return errors.New("unsupported update journal schema")
	}
	if journal.Status != StatusApplying && journal.Status != StatusRollingBack &&
		journal.Status != StatusRolledBack && journal.Status != StatusRollbackFailed && journal.Status != StatusComplete {
		return errors.New("invalid update journal status")
	}
	if _, err := ParseVersion(journal.CurrentVersion); err != nil {
		return fmt.Errorf("invalid journal current version: %w", err)
	}
	if _, err := ParseVersion(journal.TargetVersion); err != nil {
		return fmt.Errorf("invalid journal target version: %w", err)
	}
	if comparison, err := CompareVersions(journal.CurrentVersion, journal.TargetVersion); err != nil || comparison >= 0 {
		return errors.New("update journal version transition is invalid")
	}
	if !journalDigestPattern.MatchString(journal.CurrentDigest) || !journalDigestPattern.MatchString(journal.TargetDigest) {
		return errors.New("update journal source digest is invalid")
	}
	startedAt, err := parseJournalTime(journal.StartedAt)
	if err != nil {
		return errors.New("update journal start time is invalid")
	}
	updatedAt, err := parseJournalTime(journal.UpdatedAt)
	if err != nil || updatedAt.Before(startedAt) {
		return errors.New("update journal update time is invalid")
	}
	terminal := journal.Status == StatusComplete || journal.Status == StatusRolledBack ||
		journal.Status == StatusRollbackFailed
	if terminal {
		completedAt, err := parseJournalTime(journal.CompletedAt)
		if err != nil || completedAt.Before(startedAt) || completedAt.Before(updatedAt) {
			return errors.New("terminal update journal completion time is invalid")
		}
	} else if journal.CompletedAt != "" {
		return errors.New("non-terminal update journal has a completion time")
	}
	if journal.ErrorCode != "" && !journalErrorCodePattern.MatchString(journal.ErrorCode) {
		return errors.New("update journal error code is invalid")
	}
	if len(journal.Stages) != len(orderedStages) {
		return errors.New("update journal stage inventory is invalid")
	}
	for index, stage := range journal.Stages {
		if stage.ID != orderedStages[index] {
			return errors.New("update journal stage ordering is invalid")
		}
		if stage.Status != StagePending && stage.Status != StageApplying &&
			stage.Status != StageFailed && stage.Status != StageComplete {
			return errors.New("update journal stage status is invalid")
		}
		if stage.Attempts < 0 || stage.Attempts > 100 {
			return errors.New("update journal stage attempts are invalid")
		}
		if stage.ErrorCode != "" && !journalErrorCodePattern.MatchString(stage.ErrorCode) {
			return errors.New("update journal error code is invalid")
		}
		switch stage.Status {
		case StagePending:
			if stage.Attempts != 0 || stage.StartedAt != "" || stage.CompletedAt != "" || stage.ErrorCode != "" {
				return errors.New("pending update stage carries execution state")
			}
		case StageApplying:
			if stage.Attempts < 1 || !validJournalTime(stage.StartedAt) || stage.CompletedAt != "" || stage.ErrorCode != "" {
				return errors.New("applying update stage state is invalid")
			}
		case StageFailed:
			if stage.Attempts < 1 || !validJournalTime(stage.StartedAt) || stage.CompletedAt != "" || stage.ErrorCode == "" {
				return errors.New("failed update stage state is invalid")
			}
		case StageComplete:
			stageStarted, startErr := parseJournalTime(stage.StartedAt)
			stageCompleted, completeErr := parseJournalTime(stage.CompletedAt)
			if stage.Attempts < 1 || startErr != nil || completeErr != nil ||
				stageCompleted.Before(stageStarted) || stage.ErrorCode != "" {
				return errors.New("completed update stage state is invalid")
			}
		}
	}
	return nil
}

func parseJournalTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return time.Time{}, errors.New("invalid journal timestamp")
	}
	return parsed, nil
}

func validJournalTime(value string) bool {
	_, err := parseJournalTime(value)
	return err == nil
}

// ValidateJournal validates persisted recovery state for bounded, read-only
// consumers such as the privileged agent status endpoint.
func ValidateJournal(journal Journal) error {
	return validateJournal(journal)
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/RTBGG/stackfort/internal/store"
	sqliteDriver "modernc.org/sqlite"
)

const (
	maxDesiredStateBytes  = 1 << 20
	maxOperationJSONBytes = 1 << 20
	maxAuditDetailsBytes  = 16 << 10
	sqliteConstraintCode  = 19
)

var (
	slugPattern             = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	phpVersionPattern       = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	actionPattern           = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	archiveReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,511}$`)
	auditForbiddenPieces    = []string{"authorization", "cookie", "credential", "csrf", "password", "privatekey", "secret", "token"}
)

// Repository applies domain invariants around the SQLite state store.
type Repository struct {
	state                   *store.Store
	now                     func() time.Time
	newID                   func() (ID, error)
	random                  io.Reader
	derivePassword          passwordDeriver
	passwordDerivationSlots chan struct{}
	authorizationSubjectKey [32]byte
	secretMasterKey         [32]byte
	secretStorageAvailable  bool
}

// NewRepository returns a repository backed by state.
func NewRepository(state *store.Store) (*Repository, error) {
	return newRepository(state, nil)
}

// NewRepositoryWithMasterKey enables envelope encryption for retrievable
// secrets. The host master key must be exactly 256 bits and live outside SQLite.
func NewRepositoryWithMasterKey(state *store.Store, masterKey []byte) (*Repository, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("core repository master key must contain exactly 32 bytes")
	}
	return newRepository(state, masterKey)
}

func newRepository(state *store.Store, masterKey []byte) (*Repository, error) {
	if state == nil {
		return nil, errors.New("core repository requires a state store")
	}
	repository := &Repository{
		state:                   state,
		now:                     time.Now,
		newID:                   NewID,
		random:                  rand.Reader,
		derivePassword:          deriveArgon2id,
		passwordDerivationSlots: make(chan struct{}, 2),
	}
	if _, err := io.ReadFull(repository.random, repository.authorizationSubjectKey[:]); err != nil {
		return nil, fmt.Errorf("generate authorization subject key: %w", err)
	}
	if len(masterKey) == len(repository.secretMasterKey) {
		copy(repository.secretMasterKey[:], masterKey)
		repository.secretStorageAvailable = true
	}
	return repository, nil
}

func (r *Repository) timestamp() time.Time {
	return r.now().UTC()
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored UTC timestamp: %w", err)
	}
	return parsed.UTC(), nil
}

func validateText(value, field string, minimum, maximum int) (string, error) {
	normalized := strings.TrimSpace(value)
	length := utf8.RuneCountInString(normalized)
	if length < minimum || length > maximum {
		return "", fmt.Errorf("%w: %s length must be between %d and %d", ErrInvalidInput, field, minimum, maximum)
	}
	return normalized, nil
}

func validateOptionalText(value, field string, maximum int) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return validateText(value, field, 1, maximum)
}

func normalizeEmail(value string) (string, string, error) {
	email, err := validateText(value, "email", 3, 254)
	if err != nil {
		return "", "", err
	}
	if strings.Count(email, "@") != 1 || strings.ContainsAny(email, " \t\r\n") {
		return "", "", fmt.Errorf("%w: email must contain one address without whitespace", ErrInvalidInput)
	}
	parts := strings.SplitN(email, "@", 2)
	if parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[1], ".") || strings.HasSuffix(parts[1], ".") {
		return "", "", fmt.Errorf("%w: email has an invalid local or domain part", ErrInvalidInput)
	}
	return email, strings.ToLower(email), nil
}

func validateSlug(value string) (string, error) {
	if !slugPattern.MatchString(value) {
		return "", fmt.Errorf("%w: slug must be lowercase ASCII, start with a letter, and contain at most 63 characters", ErrInvalidInput)
	}
	return value, nil
}

func validateAction(value, field string, maximum int) (string, error) {
	value, err := validateText(value, field, 1, maximum)
	if err != nil {
		return "", err
	}
	if !actionPattern.MatchString(value) {
		return "", fmt.Errorf("%w: %s contains unsupported characters", ErrInvalidInput, field)
	}
	return value, nil
}

func encodeObject(value map[string]any, maximum int) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: encode JSON object: %v", ErrInvalidInput, err)
	}
	if len(encoded) > maximum {
		return "", fmt.Errorf("%w: JSON object exceeds %d bytes", ErrInvalidInput, maximum)
	}

	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return "", fmt.Errorf("%w: normalize JSON object: %v", ErrInvalidInput, err)
	}
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize JSON object: %v", ErrInvalidInput, err)
	}
	return string(encoded), nil
}

func decodeObject(value string) (map[string]any, error) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, fmt.Errorf("decode stored JSON object: %w", err)
	}
	return decoded, nil
}

func encodeLimits(limits PackageLimits) (string, PackageLimits, error) {
	normalized := limits
	if err := validateLimits(&normalized); err != nil {
		return "", PackageLimits{}, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", PackageLimits{}, fmt.Errorf("%w: encode package limits: %v", ErrInvalidInput, err)
	}
	return string(encoded), normalized, nil
}

func decodeLimits(value string) (PackageLimits, error) {
	var limits PackageLimits
	if err := json.Unmarshal([]byte(value), &limits); err != nil {
		return PackageLimits{}, fmt.Errorf("decode stored package limits: %w", err)
	}
	return limits, nil
}

func validateLimits(limits *PackageLimits) error {
	counts := []struct {
		name  string
		value int64
	}{
		{"maxDomains", limits.MaxDomains},
		{"maxDatabases", limits.MaxDatabases},
		{"maxDatabaseUsers", limits.MaxDatabaseUsers},
		{"maxScheduledJobs", limits.MaxScheduledJobs},
		{"maxOciApplications", limits.MaxOCIApplications},
	}
	for _, count := range counts {
		if count.value < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrInvalidInput, count.name)
		}
	}
	if limits.MaxScheduledJobs > int64(scheduledjobs.MaximumJobsPerAccount) {
		return fmt.Errorf("%w: maxScheduledJobs must not exceed %d", ErrInvalidInput, scheduledjobs.MaximumJobsPerAccount)
	}
	if limits.MaxOCIApplications > int64(ociapps.MaximumApplicationsPerAccount) {
		return fmt.Errorf("%w: maxOciApplications must not exceed %d", ErrInvalidInput, ociapps.MaximumApplicationsPerAccount)
	}

	if err := validateOptionalRange("cpuQuotaPercent", limits.CPUQuotaPercent, 1, 100_000); err != nil {
		return err
	}
	if err := validateOptionalRange("cpuWeight", limits.CPUWeight, 1, 10_000); err != nil {
		return err
	}
	positiveLimits := []struct {
		name  string
		value *int64
	}{
		{"memoryBytes", limits.MemoryBytes},
		{"processLimit", limits.ProcessLimit},
		{"storageInodes", limits.StorageInodes},
		{"readBytesPerSecond", limits.ReadBytesPerSecond},
		{"writeBytesPerSecond", limits.WriteBytesPerSecond},
		{"readIops", limits.ReadIOPS},
		{"writeIops", limits.WriteIOPS},
		{"monthlyIngressBytes", limits.MonthlyIngressBytes},
		{"monthlyEgressBytes", limits.MonthlyEgressBytes},
		{"monthlyCombinedBytes", limits.MonthlyCombinedBytes},
	}
	for _, limit := range positiveLimits {
		if err := validateOptionalRange(limit.name, limit.value, 1, math.MaxInt64); err != nil {
			return err
		}
	}
	if err := validateOptionalRange("storageBytes", limits.StorageBytes, 1024, math.MaxInt64); err != nil {
		return err
	}
	if err := validateOptionalRange("backupStorageBytes", limits.BackupStorageBytes, 1<<20, 1<<40); err != nil {
		return err
	}
	if limits.StorageBytes != nil && *limits.StorageBytes%1024 != 0 {
		return fmt.Errorf("%w: storageBytes must be a whole number of 1024-byte quota blocks", ErrInvalidInput)
	}
	if err := validateOptionalRange("swapBytes", limits.SwapBytes, 0, math.MaxInt64); err != nil {
		return err
	}

	versions := append([]string(nil), limits.AllowedPHPVersions...)
	for _, version := range versions {
		if !phpVersionPattern.MatchString(version) {
			return fmt.Errorf("%w: unsupported PHP version format %q", ErrInvalidInput, version)
		}
	}
	slices.Sort(versions)
	versions = slices.Compact(versions)
	if versions == nil {
		versions = []string{}
	}
	limits.AllowedPHPVersions = versions
	return nil
}

func validateOptionalRange(name string, value *int64, minimum, maximum int64) error {
	if value != nil && (*value < minimum || *value > maximum) {
		return fmt.Errorf("%w: %s must be between %d and %d", ErrInvalidInput, name, minimum, maximum)
	}
	return nil
}

func classifyDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w", ErrNotFound)
	}
	var sqliteError *sqliteDriver.Error
	if errors.As(err, &sqliteError) && sqliteError.Code()&0xff == sqliteConstraintCode {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

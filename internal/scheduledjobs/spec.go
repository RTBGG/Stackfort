// SPDX-License-Identifier: AGPL-3.0-or-later

// Package scheduledjobs defines the closed scheduled-account-job intent shared
// by the control plane, privileged agent, and systemd renderer. It deliberately
// accepts no shell command, absolute path, environment, unit name, or binary.
package scheduledjobs

import (
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

const (
	MaximumJobsPerAccount  = 1_000
	MaximumScriptPathBytes = 255
)

var ErrInvalidSpec = errors.New("invalid scheduled job specification")

type Runtime string

const (
	RuntimeShell Runtime = "shell"
	RuntimePHP   Runtime = "php"
)

type ScheduleKind string

const (
	ScheduleInterval ScheduleKind = "interval"
	ScheduleHourly   ScheduleKind = "hourly"
	ScheduleDaily    ScheduleKind = "daily"
	ScheduleWeekly   ScheduleKind = "weekly"
)

type Weekday string

const (
	Monday    Weekday = "mon"
	Tuesday   Weekday = "tue"
	Wednesday Weekday = "wed"
	Thursday  Weekday = "thu"
	Friday    Weekday = "fri"
	Saturday  Weekday = "sat"
	Sunday    Weekday = "sun"
)

type Schedule struct {
	Kind            ScheduleKind `json:"kind"`
	IntervalMinutes uint16       `json:"intervalMinutes"`
	HourUTC         uint8        `json:"hourUtc"`
	MinuteUTC       uint8        `json:"minuteUtc"`
	Weekday         Weekday      `json:"weekday,omitempty"`
}

type Definition struct {
	ID         string   `json:"id"`
	Runtime    Runtime  `json:"runtime"`
	ScriptPath string   `json:"scriptPath"`
	PHPVersion string   `json:"phpVersion,omitempty"`
	Schedule   Schedule `json:"schedule"`
	Enabled    bool     `json:"enabled"`
}

type Spec struct {
	Identity   hostingidentity.Spec `json:"identity"`
	Definition Definition           `json:"definition"`
}

type RuntimeProfile struct {
	DistributionID string
	Runtime        Runtime
	PHPVersion     string
	Executable     string
}

var canonicalUUIDv7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var safeScriptPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func Validate(spec Spec) error {
	if hostingidentity.Validate(spec.Identity) != nil || ValidateDefinition(spec.Definition) != nil {
		return ErrInvalidSpec
	}
	return nil
}

func ValidateDefinition(definition Definition) error {
	if !canonicalUUIDv7Pattern.MatchString(definition.ID) || ValidateSchedule(definition.Schedule) != nil {
		return ErrInvalidSpec
	}
	normalized, err := hostingpath.NormalizeDocumentRoot(definition.ScriptPath)
	// ExecStart is rendered from this path. Keeping the accepted alphabet
	// deliberately smaller than the file manager's avoids systemd specifier,
	// environment, quoting, and line-continuation interpretation entirely.
	if err != nil || normalized != definition.ScriptPath || len(definition.ScriptPath) > MaximumScriptPathBytes ||
		!safeScriptPathPattern.MatchString(definition.ScriptPath) {
		return ErrInvalidSpec
	}
	switch definition.Runtime {
	case RuntimeShell:
		if definition.PHPVersion != "" || !strings.HasSuffix(definition.ScriptPath, ".sh") {
			return ErrInvalidSpec
		}
	case RuntimePHP:
		if phpruntime.ValidateVersion(definition.PHPVersion) != nil || !strings.HasSuffix(definition.ScriptPath, ".php") {
			return ErrInvalidSpec
		}
	default:
		return ErrInvalidSpec
	}
	return nil
}

func ValidateSchedule(schedule Schedule) error {
	if schedule.HourUTC > 23 || schedule.MinuteUTC > 59 {
		return ErrInvalidSpec
	}
	switch schedule.Kind {
	case ScheduleInterval:
		if schedule.HourUTC != 0 || schedule.MinuteUTC != 0 || schedule.Weekday != "" ||
			(schedule.IntervalMinutes != 5 && schedule.IntervalMinutes != 15 && schedule.IntervalMinutes != 30) {
			return ErrInvalidSpec
		}
	case ScheduleHourly:
		if schedule.IntervalMinutes != 0 || schedule.HourUTC != 0 || schedule.Weekday != "" {
			return ErrInvalidSpec
		}
	case ScheduleDaily:
		if schedule.IntervalMinutes != 0 || schedule.Weekday != "" {
			return ErrInvalidSpec
		}
	case ScheduleWeekly:
		if schedule.IntervalMinutes != 0 || !validWeekday(schedule.Weekday) {
			return ErrInvalidSpec
		}
	default:
		return ErrInvalidSpec
	}
	return nil
}

func Calendar(schedule Schedule) (string, error) {
	if ValidateSchedule(schedule) != nil {
		return "", ErrInvalidSpec
	}
	timeOfDay := twoDigits(schedule.HourUTC) + ":" + twoDigits(schedule.MinuteUTC) + ":00 UTC"
	switch schedule.Kind {
	case ScheduleInterval:
		return "*-*-* *:00/" + strconv.FormatUint(uint64(schedule.IntervalMinutes), 10) + ":00 UTC", nil
	case ScheduleHourly:
		return "*-*-* *:" + twoDigits(schedule.MinuteUTC) + ":00 UTC", nil
	case ScheduleDaily:
		return "*-*-* " + timeOfDay, nil
	case ScheduleWeekly:
		return weekdayCalendarName(schedule.Weekday) + " *-*-* " + timeOfDay, nil
	default:
		return "", ErrInvalidSpec
	}
}

func Profile(distributionID string, definition Definition) (RuntimeProfile, error) {
	if ValidateDefinition(definition) != nil {
		return RuntimeProfile{}, ErrInvalidSpec
	}
	switch definition.Runtime {
	case RuntimeShell:
		return RuntimeProfile{DistributionID: distributionID, Runtime: RuntimeShell, Executable: "/bin/sh"}, nil
	case RuntimePHP:
		if _, err := phpruntime.ForDistribution(distributionID, definition.PHPVersion); err != nil {
			return RuntimeProfile{}, ErrInvalidSpec
		}
		executable := "/usr/bin/php"
		if distributionID == "debian" || distributionID == "ubuntu" {
			executable += definition.PHPVersion
		}
		return RuntimeProfile{
			DistributionID: distributionID, Runtime: RuntimePHP,
			PHPVersion: definition.PHPVersion, Executable: executable,
		}, nil
	default:
		return RuntimeProfile{}, ErrInvalidSpec
	}
}

func UnitNames(identity hostingidentity.Spec, jobID string) (service string, timer string, err error) {
	if hostingidentity.Validate(identity) != nil || !canonicalUUIDv7Pattern.MatchString(jobID) {
		return "", "", ErrInvalidSpec
	}
	base := "stackfort-job-" + strconv.FormatUint(uint64(identity.UID), 10) + "-" + strings.ReplaceAll(jobID, "-", "")
	return base + ".service", base + ".timer", nil
}

func ScriptAbsolutePath(identity hostingidentity.Spec, relative string) (string, error) {
	if hostingidentity.Validate(identity) != nil {
		return "", ErrInvalidSpec
	}
	normalized, err := hostingpath.NormalizeDocumentRoot(relative)
	if err != nil || normalized != relative {
		return "", ErrInvalidSpec
	}
	return path.Join(identity.HomeDirectory, relative), nil
}

func InvocationValues(identity hostingidentity.Spec, jobID string) ([]string, error) {
	if _, _, err := UnitNames(identity, jobID); err != nil {
		return nil, err
	}
	return []string{
		identity.AccountID, identity.Username, strconv.FormatUint(uint64(identity.UID), 10),
		strconv.FormatUint(uint64(identity.GID), 10), identity.HomeDirectory, jobID,
	}, nil
}

func FromInvocationValues(values []string) (hostingidentity.Spec, string, error) {
	if len(values) != 6 {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	uid, uidErr := strconv.ParseUint(values[2], 10, 32)
	gid, gidErr := strconv.ParseUint(values[3], 10, 32)
	identity := hostingidentity.Spec{
		AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid), HomeDirectory: values[4],
	}
	if uidErr != nil || gidErr != nil {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	if _, _, err := UnitNames(identity, values[5]); err != nil {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	return identity, values[5], nil
}

func twoDigits(value uint8) string {
	if value < 10 {
		return "0" + strconv.FormatUint(uint64(value), 10)
	}
	return strconv.FormatUint(uint64(value), 10)
}

func validWeekday(value Weekday) bool {
	return value == Monday || value == Tuesday || value == Wednesday || value == Thursday ||
		value == Friday || value == Saturday || value == Sunday
}

func weekdayCalendarName(value Weekday) string {
	return map[Weekday]string{
		Monday: "Mon", Tuesday: "Tue", Wednesday: "Wed", Thursday: "Thu",
		Friday: "Fri", Saturday: "Sat", Sunday: "Sun",
	}[value]
}

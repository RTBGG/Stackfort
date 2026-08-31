// SPDX-License-Identifier: AGPL-3.0-or-later

package hostidentity

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const maximumAccountDatabaseBytes = 4 << 20

type localUser struct {
	Name          string
	UID           uint32
	GID           uint32
	HomeDirectory string
	Shell         string
}

type localGroup struct {
	Name string
	GID  uint32
}

type accountSnapshot struct {
	usersByName  map[string]localUser
	usersByID    map[uint32]localUser
	groupsByName map[string]localGroup
	groupsByID   map[uint32]localGroup
}

type fileAccountLookup struct {
	passwdPath string
	groupPath  string
}

func newFileAccountLookup() *fileAccountLookup {
	return &fileAccountLookup{passwdPath: "/etc/passwd", groupPath: "/etc/group"}
}

func (lookup *fileAccountLookup) Load(context.Context) (accountSnapshot, error) {
	users, err := readUsers(lookup.passwdPath)
	if err != nil {
		return accountSnapshot{}, err
	}
	groups, err := readGroups(lookup.groupPath)
	if err != nil {
		return accountSnapshot{}, err
	}
	snapshot := accountSnapshot{
		usersByName: make(map[string]localUser, len(users)), usersByID: make(map[uint32]localUser, len(users)),
		groupsByName: make(map[string]localGroup, len(groups)), groupsByID: make(map[uint32]localGroup, len(groups)),
	}
	for _, user := range users {
		if _, exists := snapshot.usersByName[user.Name]; exists {
			return accountSnapshot{}, ErrInvalidDatabase
		}
		if _, exists := snapshot.usersByID[user.UID]; exists {
			return accountSnapshot{}, ErrInvalidDatabase
		}
		snapshot.usersByName[user.Name], snapshot.usersByID[user.UID] = user, user
	}
	for _, group := range groups {
		if _, exists := snapshot.groupsByName[group.Name]; exists {
			return accountSnapshot{}, ErrInvalidDatabase
		}
		if _, exists := snapshot.groupsByID[group.GID]; exists {
			return accountSnapshot{}, ErrInvalidDatabase
		}
		snapshot.groupsByName[group.Name], snapshot.groupsByID[group.GID] = group, group
	}
	return snapshot, nil
}

func readUsers(path string) ([]localUser, error) {
	var users []localUser
	err := scanAccountFile(path, func(fields []string) error {
		if len(fields) != 7 {
			return ErrInvalidDatabase
		}
		uid, err := parseNumericID(fields[2])
		if err != nil {
			return err
		}
		gid, err := parseNumericID(fields[3])
		if err != nil {
			return err
		}
		users = append(users, localUser{
			Name: fields[0], UID: uid, GID: gid, HomeDirectory: fields[5], Shell: fields[6],
		})
		return nil
	})
	return users, err
}

func readGroups(path string) ([]localGroup, error) {
	var groups []localGroup
	err := scanAccountFile(path, func(fields []string) error {
		if len(fields) != 4 {
			return ErrInvalidDatabase
		}
		gid, err := parseNumericID(fields[2])
		if err != nil {
			return err
		}
		groups = append(groups, localGroup{Name: fields[0], GID: gid})
		return nil
	})
	return groups, err
}

func scanAccountFile(path string, consume func([]string) error) error {
	// #nosec G304 -- production constructs this reader only with the fixed
	// /etc/passwd and /etc/group paths; alternate paths are test fixtures.
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open local account database", ErrInvalidDatabase)
	}
	defer file.Close()
	reader := io.LimitReader(file, maximumAccountDatabaseBytes+1)
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64<<10)
	scanner.Buffer(buffer, 64<<10)
	consumed := 0
	for scanner.Scan() {
		consumed += len(scanner.Bytes()) + 1
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			continue
		}
		if strings.IndexByte(line, 0) >= 0 {
			return ErrInvalidDatabase
		}
		if err := consume(strings.Split(line, ":")); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil || consumed > maximumAccountDatabaseBytes {
		return ErrInvalidDatabase
	}
	return nil
}

func parseNumericID(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, ErrInvalidDatabase
	}
	return uint32(parsed), nil
}

// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	activationSchemaVersion = 1
	activationManifestName  = ".stackfort-revision.json"
	maximumActivationRecord = 16 << 10
)

type activationPhase string

const (
	phasePreparing  activationPhase = "preparing"
	phaseStaged     activationPhase = "staged"
	phaseValidated  activationPhase = "validated"
	phasePromoted   activationPhase = "promoted"
	phaseReloaded   activationPhase = "reloaded"
	phaseHealthy    activationPhase = "healthy"
	phaseRecovering activationPhase = "recovering"
)

type activationJournal struct {
	SchemaVersion          int             `json:"schemaVersion"`
	Phase                  activationPhase `json:"phase"`
	RevisionID             string          `json:"revisionId"`
	PreviousRevisionID     string          `json:"previousRevisionId,omitempty"`
	AccountID              string          `json:"accountId"`
	DesiredStateRevisionID string          `json:"desiredStateRevisionId"`
}

type revisionManifest struct {
	SchemaVersion          int    `json:"schemaVersion"`
	RevisionID             string `json:"revisionId"`
	PreviousRevisionID     string `json:"previousRevisionId,omitempty"`
	AccountID              string `json:"accountId"`
	DesiredStateRevisionID string `json:"desiredStateRevisionId"`
	ConfigDigest           string `json:"configDigest"`
	RenderedDomains        int    `json:"renderedDomains"`
}

type linuxActivationStore struct{ root string }

type linuxActivationWorkspace struct {
	store    *linuxActivationStore
	lockFile *os.File
}

type linuxActivationRecovery struct {
	workspace      *linuxActivationWorkspace
	journal        activationJournal
	reloadRequired bool
}

type linuxActivationChange struct {
	workspace *linuxActivationWorkspace
	journal   activationJournal
}

func newActivationStore() activationStore { return &linuxActivationStore{root: "/"} }

func (store *linuxActivationStore) rooted(path string) string {
	return filepath.Join(store.root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
}

func (store *linuxActivationStore) Begin() (activationWorkspace, error) {
	manager := &linuxConfigurationManager{root: store.root}
	managed, err := manager.Managed()
	if err != nil || !managed {
		return nil, ErrConflict
	}
	lockPath := store.rooted(nginxbaseline.ActivationLockPath)
	descriptor, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	lockFile := os.NewFile(uintptr(descriptor), lockPath)
	closeLock := func() { _ = lockFile.Close() }
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); err != nil || stat.Uid != 0 || stat.Gid != 0 ||
		stat.Nlink != 1 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		closeLock()
		return nil, ErrConflict
	}
	if err := unix.Fchmod(descriptor, 0o600); err != nil {
		closeLock()
		return nil, err
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeLock()
		return nil, ErrConflict
	}
	workspace := &linuxActivationWorkspace{store: store, lockFile: lockFile}
	if err := workspace.ensureLayout(); err != nil {
		_ = workspace.Close()
		return nil, err
	}
	return workspace, nil
}

func (workspace *linuxActivationWorkspace) Close() error {
	if workspace == nil || workspace.lockFile == nil {
		return nil
	}
	descriptor := int(workspace.lockFile.Fd())
	unlockErr := unix.Flock(descriptor, unix.LOCK_UN)
	closeErr := workspace.lockFile.Close()
	workspace.lockFile = nil
	return errors.Join(unlockErr, closeErr)
}

func (workspace *linuxActivationWorkspace) ensureLayout() error {
	for _, directory := range []string{
		nginxbaseline.SiteRevisionsDirectory, nginxbaseline.SiteTransactionsDirectory,
	} {
		if _, err := ensureRootDirectory(workspace.store.rooted(directory), 0o750); err != nil {
			return err
		}
	}
	_, err := ensureRootFile(
		workspace.store.rooted(nginxbaseline.SitesCurrentIncludePath),
		[]byte(nginxbaseline.SitesCurrentInclude()), 0o640,
	)
	return err
}

func (workspace *linuxActivationWorkspace) Recover() (activationRecovery, error) {
	journal, exists, err := workspace.readJournal()
	if err != nil || !exists {
		return nil, err
	}
	current, err := workspace.currentRevision()
	if err != nil {
		return nil, err
	}
	reloadRequired := journal.Phase == phaseRecovering
	switch current {
	case journal.RevisionID:
		if err := workspace.replaceCurrent(journal.PreviousRevisionID); err != nil {
			return nil, err
		}
		journal.Phase = phaseRecovering
		if err := workspace.writeJournal(journal); err != nil {
			return nil, err
		}
		reloadRequired = true
	case journal.PreviousRevisionID:
		if journal.Phase == phasePromoted || journal.Phase == phaseReloaded ||
			journal.Phase == phaseHealthy || journal.Phase == phaseRecovering {
			reloadRequired = true
			journal.Phase = phaseRecovering
			if err := workspace.writeJournal(journal); err != nil {
				return nil, err
			}
		}
	default:
		return nil, ErrConflict
	}
	return &linuxActivationRecovery{
		workspace: workspace, journal: journal, reloadRequired: reloadRequired,
	}, nil
}

func (recovery *linuxActivationRecovery) ReloadRequired() bool { return recovery.reloadRequired }

func (recovery *linuxActivationRecovery) Complete() error {
	return recovery.workspace.cleanupTransaction(recovery.journal)
}

func (workspace *linuxActivationWorkspace) Active(candidate activationCandidate) (activeRevision, error) {
	current, err := workspace.currentRevision()
	if err != nil || current == "" || current != candidate.revisionID {
		return activeRevision{revisionID: current}, err
	}
	manifest, err := workspace.readManifest(current)
	if err != nil {
		return activeRevision{}, err
	}
	if manifest.RevisionID != candidate.revisionID || manifest.AccountID != candidate.accountID ||
		manifest.DesiredStateRevisionID != candidate.desiredStateRevisionID ||
		manifest.ConfigDigest != hex.EncodeToString(candidate.digest[:]) ||
		manifest.RenderedDomains != candidate.renderedDomains {
		return activeRevision{}, ErrConflict
	}
	accountPath := filepath.Join(workspace.revisionPath(current), candidate.fileName)
	if candidate.renderedDomains == 0 {
		if _, err := os.Lstat(accountPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
			return activeRevision{}, ErrConflict
		}
	} else {
		content, info, err := readSafeRootFile(accountPath)
		if err != nil || info.Mode().Perm() != 0o640 || sha256.Sum256(content) != candidate.contentDigest {
			return activeRevision{}, ErrConflict
		}
	}
	return activeRevision{
		matched: true, revisionID: current, previousRevisionID: manifest.PreviousRevisionID,
	}, nil
}

func (workspace *linuxActivationWorkspace) Stage(
	baselineSpec nginxbaseline.Spec,
	candidate activationCandidate,
) (activationChange, error) {
	if !canonicalUUIDv7(candidate.revisionID) || !canonicalUUIDv7(candidate.accountID) ||
		!canonicalUUIDv7(candidate.desiredStateRevisionID) ||
		candidate.fileName != "account-"+candidate.accountID+".conf" {
		return nil, ErrConflict
	}
	if _, exists, err := workspace.readJournal(); err != nil || exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrConflict
	}
	previous, err := workspace.currentRevision()
	if err != nil {
		return nil, err
	}
	revisionPath := workspace.revisionPath(candidate.revisionID)
	transactionPath := workspace.transactionPath(candidate.revisionID)
	if _, err := os.Lstat(revisionPath); !errors.Is(err, fs.ErrNotExist) {
		return nil, ErrConflict
	}
	if _, err := os.Lstat(transactionPath); !errors.Is(err, fs.ErrNotExist) {
		return nil, ErrConflict
	}
	journal := activationJournal{
		SchemaVersion: activationSchemaVersion, Phase: phasePreparing,
		RevisionID: candidate.revisionID, PreviousRevisionID: previous,
		AccountID: candidate.accountID, DesiredStateRevisionID: candidate.desiredStateRevisionID,
	}
	if err := workspace.writeJournal(journal); err != nil {
		return nil, err
	}
	if _, err := ensureRootDirectory(revisionPath, 0o750); err != nil {
		return nil, err
	}
	if _, err := ensureRootDirectory(transactionPath, 0o750); err != nil {
		return nil, err
	}
	if previous != "" {
		if err := workspace.copyActiveRevision(previous, revisionPath, candidate.fileName); err != nil {
			return nil, err
		}
	}
	if candidate.renderedDomains > 0 {
		if _, err := ensureRootFile(filepath.Join(revisionPath, candidate.fileName), candidate.content, 0o640); err != nil {
			return nil, err
		}
	}
	manifest := revisionManifest{
		SchemaVersion: activationSchemaVersion, RevisionID: candidate.revisionID,
		PreviousRevisionID: previous, AccountID: candidate.accountID,
		DesiredStateRevisionID: candidate.desiredStateRevisionID,
		ConfigDigest:           hex.EncodeToString(candidate.digest[:]), RenderedDomains: candidate.renderedDomains,
	}
	if err := workspace.writeManifest(revisionPath, manifest); err != nil {
		return nil, err
	}
	main, err := nginxbaseline.CandidateMain(baselineSpec, candidate.revisionID)
	if err != nil {
		return nil, err
	}
	if _, err := ensureRootFile(filepath.Join(transactionPath, "nginx.conf"), []byte(main), 0o640); err != nil {
		return nil, err
	}
	journal.Phase = phaseStaged
	if err := workspace.writeJournal(journal); err != nil {
		return nil, err
	}
	return &linuxActivationChange{workspace: workspace, journal: journal}, nil
}

func (change *linuxActivationChange) PreviousRevisionID() string {
	return change.journal.PreviousRevisionID
}

func (change *linuxActivationChange) MarkValidated() error { return change.mark(phaseValidated) }
func (change *linuxActivationChange) MarkReloaded() error  { return change.mark(phaseReloaded) }
func (change *linuxActivationChange) MarkHealthy() error   { return change.mark(phaseHealthy) }

func (change *linuxActivationChange) mark(phase activationPhase) error {
	change.journal.Phase = phase
	return change.workspace.writeJournal(change.journal)
}

func (change *linuxActivationChange) Promote() error {
	current, err := change.workspace.currentRevision()
	if err != nil || current != change.journal.PreviousRevisionID {
		return ErrConflict
	}
	if err := change.workspace.replaceCurrent(change.journal.RevisionID); err != nil {
		return err
	}
	return change.mark(phasePromoted)
}

func (change *linuxActivationChange) Abort() error {
	current, err := change.workspace.currentRevision()
	if err != nil {
		return err
	}
	if current == change.journal.RevisionID {
		return ErrConflict
	}
	if current != change.journal.PreviousRevisionID {
		return ErrConflict
	}
	return change.workspace.cleanupTransaction(change.journal)
}

func (change *linuxActivationChange) RestorePrevious() error {
	current, err := change.workspace.currentRevision()
	if err != nil {
		return err
	}
	switch current {
	case change.journal.RevisionID:
		if err := change.workspace.replaceCurrent(change.journal.PreviousRevisionID); err != nil {
			return err
		}
	case change.journal.PreviousRevisionID:
	default:
		return ErrConflict
	}
	return change.mark(phaseRecovering)
}

func (change *linuxActivationChange) CompleteRollback() error {
	return change.workspace.cleanupTransaction(change.journal)
}

func (change *linuxActivationChange) Commit() error {
	current, err := change.workspace.currentRevision()
	if err != nil || current != change.journal.RevisionID || change.journal.Phase != phaseHealthy {
		return ErrConflict
	}
	if err := change.workspace.removeManagedDirectory(change.workspace.transactionPath(change.journal.RevisionID)); err != nil {
		return err
	}
	return change.workspace.removeJournal()
}

func (workspace *linuxActivationWorkspace) copyActiveRevision(
	revisionID string,
	targetDirectory string,
	replacedFile string,
) error {
	source := workspace.revisionPath(revisionID)
	info, err := os.Lstat(source)
	if err != nil || !safeRootDirectory(info) || info.Mode().Perm() != 0o750 {
		return ErrConflict
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == activationManifestName || entry.Name() == replacedFile {
			continue
		}
		if !validAccountConfigurationName(entry.Name()) {
			return ErrConflict
		}
		content, fileInfo, err := readSafeRootFile(filepath.Join(source, entry.Name()))
		if err != nil || fileInfo.Mode().Perm() != 0o640 {
			return ErrConflict
		}
		if _, err := ensureRootFile(filepath.Join(targetDirectory, entry.Name()), content, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func (workspace *linuxActivationWorkspace) currentRevision() (string, error) {
	link := workspace.store.rooted(nginxbaseline.CurrentSitesLink)
	info, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	uid, gid, safe := rootOwnership(info)
	if !safe || uid != 0 || gid != 0 || info.Mode()&os.ModeSymlink == 0 {
		return "", ErrConflict
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	prefix := "site-revisions" + string(filepath.Separator)
	if !strings.HasPrefix(target, prefix) || strings.Contains(target, "..") {
		return "", ErrConflict
	}
	revisionID := strings.TrimPrefix(target, prefix)
	if !canonicalUUIDv7(revisionID) || filepath.Clean(target) != target {
		return "", ErrConflict
	}
	revisionInfo, err := os.Lstat(workspace.revisionPath(revisionID))
	if err != nil || !safeRootDirectory(revisionInfo) || revisionInfo.Mode().Perm() != 0o750 {
		return "", ErrConflict
	}
	return revisionID, nil
}

func (workspace *linuxActivationWorkspace) replaceCurrent(revisionID string) error {
	link := workspace.store.rooted(nginxbaseline.CurrentSitesLink)
	if revisionID == "" {
		if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(link))
	}
	if !canonicalUUIDv7(revisionID) {
		return ErrConflict
	}
	info, err := os.Lstat(workspace.revisionPath(revisionID))
	if err != nil || !safeRootDirectory(info) || info.Mode().Perm() != 0o750 {
		return ErrConflict
	}
	temporary := filepath.Join(filepath.Dir(link), ".sites-current-"+revisionID)
	_ = os.Remove(temporary)
	if err := os.Symlink(filepath.Join("site-revisions", revisionID), temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Lchown(temporary, 0, 0); err != nil {
		return err
	}
	if err := os.Rename(temporary, link); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(link))
}

func (workspace *linuxActivationWorkspace) readJournal() (activationJournal, bool, error) {
	path := workspace.store.rooted(nginxbaseline.ActivationJournalPath)
	content, info, err := readSafeRootFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return activationJournal{}, false, nil
	}
	if err != nil || info.Mode().Perm() != 0o600 || len(content) > maximumActivationRecord {
		return activationJournal{}, false, ErrConflict
	}
	var journal activationJournal
	if err := decodeStrictJSON(content, &journal); err != nil || validateJournal(journal) != nil {
		return activationJournal{}, false, ErrConflict
	}
	return journal, true, nil
}

func (workspace *linuxActivationWorkspace) writeJournal(journal activationJournal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	content, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return atomicWrite(
		workspace.store.rooted(nginxbaseline.ActivationJournalPath), content, 0o600, 0, 0,
	)
}

func (workspace *linuxActivationWorkspace) removeJournal() error {
	path := workspace.store.rooted(nginxbaseline.ActivationJournalPath)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (workspace *linuxActivationWorkspace) writeManifest(path string, manifest revisionManifest) error {
	content, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return atomicWrite(filepath.Join(path, activationManifestName), content, 0o640, 0, 0)
}

func (workspace *linuxActivationWorkspace) readManifest(revisionID string) (revisionManifest, error) {
	content, info, err := readSafeRootFile(filepath.Join(workspace.revisionPath(revisionID), activationManifestName))
	if err != nil || info.Mode().Perm() != 0o640 || len(content) > maximumActivationRecord {
		return revisionManifest{}, ErrConflict
	}
	var manifest revisionManifest
	if err := decodeStrictJSON(content, &manifest); err != nil || validateManifest(manifest) != nil ||
		manifest.RevisionID != revisionID {
		return revisionManifest{}, ErrConflict
	}
	return manifest, nil
}

func (workspace *linuxActivationWorkspace) cleanupTransaction(journal activationJournal) error {
	current, err := workspace.currentRevision()
	if err != nil || current == journal.RevisionID {
		return ErrConflict
	}
	var failures []error
	if err := workspace.removeManagedDirectory(workspace.transactionPath(journal.RevisionID)); err != nil {
		failures = append(failures, err)
	}
	if err := workspace.removeManagedDirectory(workspace.revisionPath(journal.RevisionID)); err != nil {
		failures = append(failures, err)
	}
	if len(failures) == 0 {
		if err := workspace.removeJournal(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (workspace *linuxActivationWorkspace) removeManagedDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !safeRootDirectory(info) || info.Mode().Perm() != 0o750 {
		return ErrConflict
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		filePath := filepath.Join(path, entry.Name())
		_, _, err := readSafeRootFile(filePath)
		if err != nil {
			return ErrConflict
		}
		if err := os.Remove(filePath); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (workspace *linuxActivationWorkspace) revisionPath(revisionID string) string {
	return filepath.Join(workspace.store.rooted(nginxbaseline.SiteRevisionsDirectory), revisionID)
}

func (workspace *linuxActivationWorkspace) transactionPath(revisionID string) string {
	return filepath.Join(workspace.store.rooted(nginxbaseline.SiteTransactionsDirectory), revisionID)
}

func validateJournal(journal activationJournal) error {
	if journal.SchemaVersion != activationSchemaVersion || !canonicalUUIDv7(journal.RevisionID) ||
		!canonicalUUIDv7(journal.AccountID) || !canonicalUUIDv7(journal.DesiredStateRevisionID) ||
		journal.RevisionID == journal.PreviousRevisionID {
		return ErrConflict
	}
	if journal.PreviousRevisionID != "" && !canonicalUUIDv7(journal.PreviousRevisionID) {
		return ErrConflict
	}
	switch journal.Phase {
	case phasePreparing, phaseStaged, phaseValidated, phasePromoted,
		phaseReloaded, phaseHealthy, phaseRecovering:
		return nil
	default:
		return ErrConflict
	}
}

func validateManifest(manifest revisionManifest) error {
	if manifest.SchemaVersion != activationSchemaVersion || !canonicalUUIDv7(manifest.RevisionID) ||
		!canonicalUUIDv7(manifest.AccountID) || !canonicalUUIDv7(manifest.DesiredStateRevisionID) ||
		manifest.RenderedDomains < 0 {
		return ErrConflict
	}
	if manifest.PreviousRevisionID != "" && !canonicalUUIDv7(manifest.PreviousRevisionID) {
		return ErrConflict
	}
	digest, err := hex.DecodeString(manifest.ConfigDigest)
	if err != nil || len(digest) != sha256.Size || manifest.ConfigDigest != hex.EncodeToString(digest) {
		return ErrConflict
	}
	return nil
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}

func validAccountConfigurationName(name string) bool {
	if !strings.HasPrefix(name, "account-") || !strings.HasSuffix(name, ".conf") {
		return false
	}
	return canonicalUUIDv7(strings.TrimSuffix(strings.TrimPrefix(name, "account-"), ".conf"))
}

func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrConflict
	}
	return nil
}

func syncDirectory(path string) error {
	// #nosec G304 -- callers pass parents derived from fixed activation-store paths and canonical UUIDv7 names.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

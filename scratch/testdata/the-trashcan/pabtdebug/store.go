/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabtdebug

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// Errors the store returns. They are distinguishable so a caller can tell a
// conflict from an unavailable store, which are different situations and need
// different responses.
var (
	// ErrStoreClosed reports use of a store that has been closed.
	ErrStoreClosed = errors.New(`pabtdebug: store closed`)

	// ErrStoreReadOnly reports a write attempted against a read-only store.
	ErrStoreReadOnly = errors.New(`pabtdebug: store is read-only`)

	// ErrStoreLocked reports a second writer on the same database.
	ErrStoreLocked = errors.New(`pabtdebug: another writer holds the store`)

	// ErrUnsupportedSchema reports a database written by a newer build.
	ErrUnsupportedSchema = errors.New(`pabtdebug: unsupported store schema`)

	// ErrAppendBusy reports an append attempted while a tick already holds the
	// run's lease.
	ErrAppendBusy = errors.New(`pabtdebug: the run's append lease is held`)

	// ErrHeadMismatch reports a commit whose view of the run head was stale.
	ErrHeadMismatch = errors.New(`pabtdebug: run head moved`)

	// ErrNotFound reports an absent row.
	ErrNotFound = errors.New(`pabtdebug: not found`)

	// ErrStoreCorrupt reports a file that failed its integrity check.
	ErrStoreCorrupt = errors.New(`pabtdebug: store is corrupt`)
)

// StoreMode says whether a store accepts writes.
type StoreMode uint8

const (
	// StoreModeReadWrite is the only mode that writes.
	StoreModeReadWrite StoreMode = iota + 1

	// StoreModeReadOnly opens an existing store for inspection.
	StoreModeReadOnly
)

// StoreLimits bound what the store will hold and return.
type StoreLimits struct {
	// MaxRecordBytes is the largest canonical record the store accepts.
	MaxRecordBytes uint64

	// MaxTreeBytes is the largest canonical tree the store accepts.
	MaxTreeBytes uint64

	// MaxPageRows is the most rows one metadata page may return.
	MaxPageRows uint32

	// MaxPageBytes is the most canonical bytes one metadata page may return.
	MaxPageBytes uint64

	// MaxReaders is the size of the read connection pool.
	MaxReaders uint32
}

// DefaultStoreLimits returns limits that suit an interactive local debugger on a
// run of a few hundred thousand ticks.
func DefaultStoreLimits() StoreLimits {
	return StoreLimits{
		MaxRecordBytes: 16 << 20,
		MaxTreeBytes:   16 << 20,
		MaxPageRows:    500,
		MaxPageBytes:   8 << 20,
		MaxReaders:     4,
	}
}

// Validate reports limits that cannot bound anything.
func (l StoreLimits) Validate() error {
	if l.MaxRecordBytes == 0 || l.MaxTreeBytes == 0 ||
		l.MaxPageRows == 0 || l.MaxPageBytes == 0 || l.MaxReaders == 0 {
		return fmt.Errorf(`pabtdebug: incomplete store limits`)
	}
	return nil
}

// SQLiteStoreConfig configures a store.
type SQLiteStoreConfig struct {
	// Path is the database file. Its directory is created owner-only if absent.
	Path string

	// Mode says whether the store writes. A nil Mode is read-write.
	Mode *StoreMode

	// Limits overrides the defaults. A nil Limits selects the defaults.
	Limits *StoreLimits

	// Clock supplies every stored timestamp. A nil Clock uses the wall clock.
	Clock func() time.Time
}

// StoreID is a random identity for one database file. It is not a secret and
// grants no access; it exists so a debugger can tell two stores apart.
type StoreID string

// RecoveryState says whether a store is fit to record into.
type RecoveryState string

const (
	// RecoveryReady means the store is consistent and open for writes.
	RecoveryReady RecoveryState = "ready"

	// RecoveryPending means an open intent was found and the caller must resolve
	// it before recording.
	RecoveryPending RecoveryState = "pending"

	// RecoveryCorrupt means the integrity check failed and the store is refused.
	RecoveryCorrupt RecoveryState = "corrupt"
)

// Installation is the store's own state.
type Installation struct {
	StoreID         StoreID
	Generation      uint64
	ActiveSessionID string
	Recovery        RecoveryState
	UpdatedAt       time.Time
}

// SessionIdentity names one session's plan and run inside a store.
type SessionIdentity struct {
	StoreID    StoreID
	SessionID  string
	PlanID     string
	RunID      string
	Generation uint64
}

// RunHead is the latest committed position of one run.
type RunHead struct {
	RunKey           RunKey
	Generation       uint64
	HeadSequence     uint64
	HeadTreeSequence uint64
	HeadTreeEventUID string
	HeadTreeDigest   string
	HeadRecordSHA256 string
	HeadPlanRevision uint64
	LastCommittedAt  time.Time
}

// BootstrapRequest names the plan and run a fresh store should create.
type BootstrapRequest struct {
	PlanID   string
	RunID    string
	Title    string
	Metadata []StoredValue
}

// BeginRequest asks for a sequence under the run's append lease.
type BeginRequest struct {
	RunKey               RunKey
	ExpectedPlanRevision uint64
}

// IntentTicket is proof that a sequence was allocated to one attempt.
//
// The proof binds the ticket's own fields together, so a commit cannot present a
// ticket whose sequence, event, or attempt does not match what it claims.
//
// It is an unkeyed checksum, so it is not a secret and a caller could recompute
// one. What it is for is catching a ticket assembled from pieces of different
// attempts, which is a mistake rather than an attack. The guard against a forged
// commit is the append lease: Commit requires the run's lease to be held by this
// ticket's own attempt, which only Begin can grant.
type IntentTicket struct {
	RunKey               RunKey
	Sequence             uint64
	EventUID             string
	AttemptID            string
	ExpectedHeadSequence uint64
	ExpectedTreeSequence uint64
	ExpectedPlanRevision uint64
	ExpectedGeneration   uint64
	// ExecutionStarted reports whether the tick had begun running the application
	// when the intent was found, which is what tells a crash during a tick from a
	// crash before it started.
	ExecutionStarted bool
	StartedAt        time.Time

	proof [32]byte
}

// CommitReceipt proves a record was committed.
type CommitReceipt struct {
	RunKey       RunKey
	Sequence     uint64
	EventUID     string
	RecordSHA256 string
	TreeSHA256   string
	CommittedAt  time.Time

	proof [32]byte
}

// EventQuery selects a page of event metadata.
type EventQuery struct {
	RunKey RunKey
	// Before, when set, returns events strictly below the sequence. After, when
	// set, returns events strictly above it.
	Before *uint64
	After  *uint64
	// Kinds, when non-empty, keeps only these record kinds.
	Kinds []RecordKind
	// Limit caps the rows. A zero limit uses the store's page bound.
	Limit uint32
}

// EventSummary is one row of the timeline. It carries no tree, so a page stays
// small however large the runs are.
type EventSummary struct {
	RunKey       RunKey
	Sequence     uint64
	EventUID     string
	Kind         RecordKind
	PayloadState PayloadState
	TreeEligible bool
	RecordSHA256 string
	TreeSHA256   string
	ContentBytes uint64
	RecordedAt   time.Time
}

// EventPage is one bounded page of the timeline.
type EventPage struct {
	RunKey    RunKey
	Events    []EventSummary
	OldestSeq uint64
	NewestSeq uint64
	HasMore   bool
}

// RecordPage is a bounded run of whole records.
//
// It is the whole record rather than the metadata Events carries, because the reader
// that needs it is a client resuming a stream, and a client cannot apply a delta it
// has not been given the record for. The bounds are the store's own.
type RecordPage struct {
	RunKey    RunKey
	Records   []StoredRecord
	OldestSeq uint64
	Through   uint64
	HasMore   bool
}

// SQLiteStore is the durable history.
//
// One write connection and a small read pool, with the connection policy in the
// data source name so every pooled connection gets it. A read-write store also
// holds an exclusive lock on a sidecar file, which is how a second writer on the
// same history is refused without a second dependency.
type SQLiteStore struct {
	path    string
	mode    StoreMode
	limits  StoreLimits
	clock   func() time.Time
	storeID StoreID

	writer *sql.DB
	// leaseDB and lease hold an exclusive transaction on a sidecar file, and are
	// nil for a read-only store.
	//
	// The lock is a second database rather than a transaction on this one, because a
	// store must be able to write while it holds the lock. A transaction on the
	// history itself would refuse this process's own writes.
	leaseDB *sql.DB
	lease   *sql.Conn
	readers *sql.DB

	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex
}

// OpenSQLiteStore opens or creates a store.
//
// A read-write open creates the file and its directory owner-only, applies the
// schema, and refuses a database written by a newer build. A read-only open
// refuses a file that does not exist rather than creating one.
func OpenSQLiteStore(ctx context.Context, cfg SQLiteStoreConfig) (*SQLiteStore, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf(`pabtdebug: store path is required`)
	}
	limits := DefaultStoreLimits()
	if cfg.Limits != nil {
		limits = *cfg.Limits
		if err := limits.Validate(); err != nil {
			return nil, err
		}
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	mode := StoreModeReadWrite
	if cfg.Mode != nil {
		mode = *cfg.Mode
	}
	path, err := canonicalStorePath(cfg.Path, mode)
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{path: path, mode: mode, limits: limits, clock: clock}
	if err := s.open(ctx); err != nil {
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		_ = s.close()
		return nil, err
	}
	if err := s.admit(ctx); err != nil {
		_ = s.close()
		return nil, err
	}
	return s, nil
}

// admit decides whether the store is fit to record into, and records what it found.
//
// A store is not simply "ready" because it opened. An intent left by an earlier
// process means a tick was allocated a sequence and never resolved, and a chain
// that does not verify means the history on disk is not what was committed. Both
// are recorded, so a caller reads the state rather than assuming it, and a chain
// that does not verify refuses a read-write open outright: a debugger that appends
// to an altered history is worse than one that will not open.
//
// A read-only open is never refused. It is being used to look at a history,
// including a damaged one, which is exactly when inspection matters most.
func (s *SQLiteStore) admit(ctx context.Context) error {
	pending, err := s.PendingIntents(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	if s.mode != StoreModeReadWrite {
		// A read-only store is being inspected, not recorded into, so the state is
		// left for the caller to read rather than acted on here.
		return nil
	}
	if err := s.setRecovery(ctx, RecoveryPending); err != nil {
		return err
	}
	// Every run that still has an open intent has its chain checked, so a caller
	// is told about corruption before it writes anything more onto the run. A run
	// with no records at all has a valid empty chain, which is the ordinary
	// outcome of a crash during a run's first tick, so it is not treated as damage.
	for _, ticket := range pending {
		if _, err := s.VerifyChain(ctx, ticket.RunKey); err != nil {
			if errors.Is(err, ErrStoreCorrupt) {
				_ = s.setRecovery(ctx, RecoveryCorrupt)
			}
			return err
		}
	}
	return nil
}

// setRecovery records the store's recovery state.
func (s *SQLiteStore) setRecovery(ctx context.Context, state RecoveryState) error {
	if _, err := s.writer.ExecContext(ctx,
		"UPDATE installation SET recovery = ?, updated_at = ? WHERE singleton = 1",
		string(state), stamp(s.clock().UTC())); err != nil {
		return fmt.Errorf("pabtdebug: record recovery state: %w", err)
	}
	return nil
}

// Path returns the database file.
func (s *SQLiteStore) Path() string { return s.path }

// Limits returns the limits this store enforces.
func (s *SQLiteStore) Limits() StoreLimits { return s.limits }

// Mode returns whether the store writes.
func (s *SQLiteStore) Mode() StoreMode { return s.mode }

// canonicalStorePath resolves the database path and makes its directory and file
// owner-only.
//
// Symlinks are resolved rather than refused. A system temporary directory is very
// often a symlink, so refusing those would refuse ordinary use, and resolving
// gives one canonical path that the lock, the mode, and a later export all agree
// on.
// resolveStorePath resolves every symlink in a store path, returning the
// canonical file and its directory.
//
// A component that does not resolve yet, which is a file about to be created,
// is left alone rather than treated as an error: the caller resolves again once
// the file exists.
func resolveStorePath(abs string) (resolved, dir string) {
	dir = filepath.Dir(abs)
	if target, err := filepath.EvalSymlinks(abs); err == nil {
		return target, filepath.Dir(target)
	}
	if target, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(target, filepath.Base(abs)), target
	}
	return abs, dir
}

func canonicalStorePath(path string, mode StoreMode) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStoreCorrupt, err)
	}
	dir := filepath.Dir(abs)
	// The whole path is resolved rather than refused when it contains a symlink. A
	// system temporary directory is very often a symlink, so refusing those would
	// refuse ordinary use; resolving gives one canonical path that the lock, the
	// mode, and a later export all agree on.
	//
	// The final component matters as much as the directory. A symlinked file would
	// otherwise let two processes open one history through two paths, and the
	// second would take its own lock and never be refused.
	abs, dir = resolveStorePath(abs)
	if mode == StoreModeReadWrite {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("pabtdebug: store directory: %w", err)
		}
		// The file is created here, owner-only, rather than left to whichever mode
		// the process umask would give it. A store is the user's data and must
		// never be briefly readable by another account, not even for the moment
		// before SQLite opens it.
		file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return "", fmt.Errorf("pabtdebug: store file: %w", err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("pabtdebug: store file: %w", err)
		}
		if err := os.Chmod(abs, 0o600); err != nil {
			return "", fmt.Errorf("pabtdebug: store file mode: %w", err)
		}
		// MkdirAll leaves an existing directory's mode alone, so it is set
		// explicitly. A store is the user's data and must not be world readable.
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", fmt.Errorf("pabtdebug: store directory mode: %w", err)
		}
		// Creating the file may have made the last component resolvable, and a
		// store opened through a dangling symlink now has a target. Resolving
		// again is what makes such a store's path, and therefore its lock, name
		// the same file as one opened through the target directly.
		abs, dir = resolveStorePath(abs)
	} else if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return abs, nil
}

func (s *SQLiteStore) open(ctx context.Context) error {
	write := s.mode == StoreModeReadWrite
	// Per-connection pragmas go in the DSN, not in an Exec.
	//
	// A pragma issued through a pool reaches whichever single connection
	// database/sql happened to hand it. Every other connection this pool mints
	// would run without it, and foreign_keys is the schema's only guard against
	// a row the chain can never account for.
	dsn := s.path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=temp_store(MEMORY)" +
		"&_pragma=cache_size(-8192)&_pragma=mmap_size(0)&_pragma=wal_autocheckpoint(1000)"
	if !write {
		dsn = s.path + "?mode=ro&_pragma=query_only(1)"
	}
	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("pabtdebug: open store: %w", err)
	}
	// One writer connection. The append lease serialises ticks, so a second
	// connection would buy nothing, and every connection the pool mints needs the
	// pragmas above, which the DSN now guarantees.
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	s.writer = writer
	if write {
		// The lock is taken before anything is written, so a second process is
		// refused before it can begin a migration.
		if err := s.acquireLease(ctx); err != nil {
			return err
		}
	}
	if err := s.checkPragmas(ctx, writer, write); err != nil {
		return err
	}
	if write {
		// The pragmas are what create the file, so its mode is set here rather than
		// before it exists. SQLite makes it with the process umask; the store makes
		// it owner-only.
		if err := os.Chmod(s.path, 0o600); err != nil {
			return fmt.Errorf("pabtdebug: store file mode: %w", err)
		}
	} else if err := requireOwnerOnly(s.path); err != nil {
		// A read-only open did not create the file, so its mode is not this
		// process's to change. A world-readable history is a disclosure, so it is
		// reported rather than served.
		return err
	}
	// The read DSN carries its own query string. A read-write store's main DSN is a
	// bare path, so appending a parameter with an ampersand would name a different
	// file rather than configure the same one.
	readDSN := s.path + "?_pragma=query_only(1)&_pragma=busy_timeout(5000)" +
		"&_pragma=cache_size(-8192)&_pragma=temp_store(MEMORY)"
	if !write {
		readDSN = s.path + "?mode=ro&_pragma=query_only(1)"
	}
	readers, err := sql.Open("sqlite", readDSN)
	if err != nil {
		return fmt.Errorf("pabtdebug: open readers: %w", err)
	}
	readers.SetMaxOpenConns(int(s.limits.MaxReaders))
	readers.SetMaxIdleConns(int(s.limits.MaxReaders))
	if err := s.checkPragmas(ctx, readers, false); err != nil {
		return err
	}
	s.readers = readers
	return nil
}

// checkPragmas verifies the connection policy rather than setting it.
//
// The policy is in the DSN because that is the only place a pragma applies to
// every connection a pool mints. This reads the settings back on one connection
// so a driver that silently ignored a DSN parameter is caught at open rather than
// at the first write that depended on it.
func (s *SQLiteStore) checkPragmas(ctx context.Context, db *sql.DB, write bool) error {
	want := map[string]int{
		"foreign_keys": 1,
		"busy_timeout": 5000,
	}
	if !write {
		want = map[string]int{"query_only": 1}
	}
	for name, expected := range want {
		var got int
		if err := db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil {
			return fmt.Errorf("pabtdebug: read %s: %w", name, err)
		}
		if got != expected {
			return fmt.Errorf("%w: %s is %d, want %d", ErrStoreCorrupt, name, got, expected)
		}
	}
	return nil
}

// acquireLease takes an exclusive lock on a sidecar database and holds it for the
// store's life.
//
// This is the second-writer refusal, and it is why the lock lives in its own file.
// A transaction on the history would refuse this process's own writes; a
// transaction on an empty sidecar refuses a second process and nothing else. Two
// processes both reach BEGIN EXCLUSIVE and the second is refused as soon as its
// short busy timeout expires.
//
// The lock is a POSIX advisory lock held by the open file, so the kernel releases
// it when a process dies. There is no stale-lock failure mode: a killed debugger
// never leaves a history that cannot be opened again.
func (s *SQLiteStore) acquireLease(ctx context.Context) error {
	lockPath := s.path + ".lock"
	if err := os.Chmod(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("pabtdebug: store directory mode: %w", err)
	}
	lock, err := sql.Open("sqlite", lockPath+"?_pragma=busy_timeout(250)&_pragma=journal_mode(OFF)")
	if err != nil {
		return fmt.Errorf("pabtdebug: open store lock: %w", err)
	}
	lock.SetMaxOpenConns(1)
	lock.SetMaxIdleConns(1)
	conn, err := lock.Conn(ctx)
	if err != nil {
		_ = lock.Close()
		// A second process is refused as soon as it tries to connect, not only
		// when it tries to begin a transaction, so this path needs the same
		// classification.
		if isBusy(err) {
			return fmt.Errorf("%w: %v", ErrStoreLocked, err)
		}
		return fmt.Errorf("pabtdebug: store lock: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(waitCtx, "BEGIN EXCLUSIVE"); err != nil {
		_ = conn.Close()
		_ = lock.Close()
		if isBusy(err) {
			return fmt.Errorf("%w: %v", ErrStoreLocked, err)
		}
		return fmt.Errorf("pabtdebug: acquire store lock: %w", err)
	}
	s.leaseDB = lock
	s.lease = conn
	// SQLite creates a file with the process umask, so the store sets the mode it
	// wants rather than refusing what it just made. The owner-only directory is the
	// primary control; these are the second.
	if err := os.Chmod(lockPath, 0o600); err != nil {
		return fmt.Errorf("pabtdebug: store lock mode: %w", err)
	}
	return nil
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "database is locked") ||
		strings.Contains(text, "database table is locked") ||
		strings.Contains(text, "SQLITE_BUSY")
}

// requireOwnerOnly refuses a file other accounts can read.
func requireOwnerOnly(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("pabtdebug: stat store: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %v, want owner-only", ErrStoreCorrupt, path, perm)
	}
	return nil
}

// migrate applies the schema and refuses a newer file.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	var version int
	row := s.writer.QueryRowContext(ctx, "PRAGMA user_version")
	if err := row.Scan(&version); err != nil {
		return fmt.Errorf("pabtdebug: read schema version: %w", err)
	}
	switch {
	case version == SchemaVersion:
		return s.verifyInstallation(ctx)
	case version == 0:
		if s.mode != StoreModeReadWrite {
			return fmt.Errorf("%w: a read-only store has no schema", ErrUnsupportedSchema)
		}
		if _, err := s.writer.ExecContext(ctx, schemaSQL); err != nil {
			return fmt.Errorf("pabtdebug: apply schema: %w", err)
		}
		if _, err := s.writer.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
			return fmt.Errorf("pabtdebug: record schema version: %w", err)
		}
		return nil
	case version > SchemaVersion:
		return fmt.Errorf("%w: file is version %d, this build writes %d",
			ErrUnsupportedSchema, version, SchemaVersion)
	default:
		// A forward migration is a step, not a guess. There is none yet, so an
		// older version is refused rather than rewritten.
		return fmt.Errorf("%w: file is version %d, this build knows %d",
			ErrUnsupportedSchema, version, SchemaVersion)
	}
}

func (s *SQLiteStore) verifyInstallation(ctx context.Context) error {
	var storeID, session string
	var generation int
	row := s.writer.QueryRowContext(ctx,
		"SELECT store_id, generation, active_session_id FROM installation WHERE singleton = 1")
	switch err := row.Scan(&storeID, &generation, &session); {
	case errors.Is(err, sql.ErrNoRows):
		// A schema with no installation row is a fresh file that was created
		// elsewhere; the store is usable and unbootstrapped.
		return nil
	case err != nil:
		return fmt.Errorf("pabtdebug: read installation: %w", err)
	}
	s.storeID = StoreID(storeID)
	return nil
}

// Installation returns the store's own state.
func (s *SQLiteStore) Installation(ctx context.Context) (Installation, error) {
	var (
		out        Installation
		storeID    string
		session    string
		generation int
		recovery   string
		updated    string
	)
	row := s.writer.QueryRowContext(ctx,
		"SELECT store_id, generation, active_session_id, recovery, updated_at "+
			"FROM installation WHERE singleton = 1")
	err := row.Scan(&storeID, &generation, &session, &recovery, &updated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Installation{}, fmt.Errorf("%w: the store is not bootstrapped", ErrNotFound)
	case err != nil:
		return Installation{}, fmt.Errorf("pabtdebug: read installation: %w", err)
	}
	out.StoreID = StoreID(storeID)
	out.Generation = uint64(generation)
	out.ActiveSessionID = session
	out.Recovery = RecoveryState(recovery)
	if parsed, perr := time.Parse(time.RFC3339Nano, updated); perr == nil {
		out.UpdatedAt = parsed.UTC()
	}
	return out, nil
}

// StoreIdentity returns the store's own identity, which is what a cursor is bound to.
func (s *SQLiteStore) StoreIdentity() StoreID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storeID
}

// randomID returns a fresh random identifier with the given prefix.
func randomID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("pabtdebug: entropy: %w", err)
	}
	return fmt.Sprintf("%s-%x", prefix, b[:]), nil
}

func (s *SQLiteStore) proof(parts ...any) [32]byte {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%v\x00", part)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Close releases the store. It is idempotent.
func (s *SQLiteStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.lease != nil {
			if _, rollbackErr := s.lease.ExecContext(context.Background(), "ROLLBACK"); rollbackErr != nil {
				err = fmt.Errorf("pabtdebug: release the store lock: %w", rollbackErr)
			}
			if closeErr := s.lease.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("pabtdebug: close the store lock: %w", closeErr)
			}
			s.lease = nil
		}
		if s.leaseDB != nil {
			if closeErr := s.leaseDB.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("pabtdebug: close the store lock: %w", closeErr)
			}
			s.leaseDB = nil
		}
		if s.readers != nil {
			if closeErr := s.readers.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("pabtdebug: close readers: %w", closeErr)
			}
		}
		if s.writer != nil {
			if closeErr := s.writer.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("pabtdebug: close store: %w", closeErr)
			}
		}
	})
	return err
}

// checkUsable reports a typed error when the store cannot be used.
func (s *SQLiteStore) checkUsable(write bool) error {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return ErrStoreClosed
	}
	if write && s.mode != StoreModeReadWrite {
		return ErrStoreReadOnly
	}
	return nil
}

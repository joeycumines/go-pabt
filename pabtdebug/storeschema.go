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

// SchemaVersion is the store shape this build writes and reads.
//
// A newer file is refused rather than guessed at, and a migration is a forward
// step applied on open. There is no downgrade path, because a debugger that
// silently downgraded a history would be reporting facts it had lost.
const SchemaVersion = 1

// schemaSQL is the whole of version 1.
//
// The tables are: one installation row, the sessions it has seen, the plans and
// runs inside them, the open intents that make a tick durable before it runs,
// the committed records, and one tree per eligible record.
//
// Projections are deliberately absent. A projection is a second copy of something
// the records already hold, so it is created where a query needs one, with a
// rebuild path, rather than declared up front. No query needs one yet.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS installation (
    singleton          INTEGER PRIMARY KEY CHECK (singleton = 1),
    store_id           TEXT    NOT NULL,
    generation         INTEGER NOT NULL CHECK (generation > 0),
    active_session_id  TEXT    NOT NULL,
    recovery           TEXT    NOT NULL
        CHECK (recovery IN ('ready', 'pending', 'corrupt')),
    updated_at         TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    session_id  TEXT PRIMARY KEY,
    store_id    TEXT    NOT NULL,
    generation  INTEGER NOT NULL CHECK (generation > 0),
    read_only   INTEGER NOT NULL CHECK (read_only IN (0, 1)),
    created_at  TEXT    NOT NULL,
    record_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS plans (
    session_id     TEXT NOT NULL,
    plan_id        TEXT NOT NULL,
    title          TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    -- The plan's recorded metadata, encoded canonically. A NULL means the plan
    -- declared none, which is distinct from declaring an empty list.
    metadata_json  BLOB,
    record_sha256  BLOB NOT NULL CHECK (length(record_sha256) = 32),
    PRIMARY KEY (session_id, plan_id),
    FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS runs (
    session_id         TEXT NOT NULL,
    plan_id            TEXT NOT NULL,
    run_id             TEXT NOT NULL,
    state              TEXT NOT NULL CHECK (state IN ('active', 'closed', 'interrupted')),
    started_at         TEXT NOT NULL,
    ended_at           TEXT,
    initial_plan_revision INTEGER NOT NULL,
    head_seq           INTEGER NOT NULL DEFAULT 0 CHECK (head_seq >= 0),
    head_tree_seq      INTEGER NOT NULL DEFAULT 0 CHECK (head_tree_seq >= 0),
    head_tree_event_uid TEXT,
    head_tree_digest   TEXT,
    head_record_sha256 BLOB,
    head_plan_revision INTEGER NOT NULL DEFAULT 0,
    -- The append lease. A non-null owner means a tick has allocated a sequence
    -- and has not yet committed or abandoned it, so no other writer may take the
    -- next one.
    append_owner       TEXT,
    append_attempt_id  TEXT,
    append_epoch       INTEGER NOT NULL DEFAULT 0,
    record_sha256      BLOB,
    PRIMARY KEY (session_id, run_id),
    UNIQUE (session_id, plan_id, run_id),
    FOREIGN KEY (session_id, plan_id) REFERENCES plans(session_id, plan_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS intents (
    session_id           TEXT NOT NULL,
    event_uid            TEXT NOT NULL,
    plan_id              TEXT NOT NULL,
    run_id               TEXT NOT NULL,
    seq                  INTEGER NOT NULL CHECK (seq > 0),
    attempt_id           TEXT NOT NULL,
    expected_head_seq    INTEGER NOT NULL,
    expected_tree_seq    INTEGER NOT NULL,
    expected_plan_revision INTEGER NOT NULL,
    expected_generation  INTEGER NOT NULL,
    execution_started    INTEGER NOT NULL CHECK (execution_started IN (0, 1)),
    started_at           TEXT NOT NULL,
    PRIMARY KEY (session_id, event_uid),
    UNIQUE (session_id, run_id, seq),
    UNIQUE (session_id, run_id, attempt_id),
    FOREIGN KEY (session_id, plan_id, run_id)
        REFERENCES runs(session_id, plan_id, run_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS records (
    session_id         TEXT NOT NULL,
    event_uid          TEXT NOT NULL,
    plan_id            TEXT NOT NULL,
    run_id             TEXT NOT NULL,
    seq                INTEGER NOT NULL CHECK (seq > 0),
    attempt_id         TEXT,
    kind               TEXT NOT NULL
        CHECK (kind IN ('tick', 'capture_failed', 'panicked', 'interrupted',
                         'aborted', 'control', 'import')),
    payload_state      TEXT NOT NULL
        CHECK (payload_state IN ('complete', 'partial', 'none')),
    tree_eligible      INTEGER NOT NULL CHECK (tree_eligible IN (0, 1)),
    execution_started  INTEGER NOT NULL CHECK (execution_started IN (0, 1)),
    recorded_at        TEXT NOT NULL,
    -- The complete canonical record, so the row is self-describing and a reader
    -- never has to reassemble it from columns.
    record_json        BLOB NOT NULL,
    payload_sha256     BLOB NOT NULL CHECK (length(payload_sha256) = 32),
    -- 64 hexadecimal zeroes for the first record of a run, the previous record's
    -- digest for every one after it. This is what makes the chain verifiable.
    previous_record_sha256 BLOB NOT NULL CHECK (length(previous_record_sha256) = 32),
    record_sha256      BLOB NOT NULL CHECK (length(record_sha256) = 32),
    PRIMARY KEY (session_id, event_uid),
    UNIQUE (session_id, run_id, seq),
    FOREIGN KEY (session_id, plan_id, run_id)
        REFERENCES runs(session_id, plan_id, run_id) ON DELETE RESTRICT,
    CHECK (tree_eligible = 0 OR payload_state = 'complete')
);

CREATE TABLE IF NOT EXISTS representations (
    session_id         TEXT NOT NULL,
    event_uid          TEXT NOT NULL,
    plan_id            TEXT NOT NULL,
    run_id             TEXT NOT NULL,
    seq                INTEGER NOT NULL CHECK (seq > 0),
    base_event_uid     TEXT,
    base_seq           INTEGER,
    target_plan_revision INTEGER NOT NULL,
    tree_json          BLOB NOT NULL,
    tree_sha256        BLOB NOT NULL CHECK (length(tree_sha256) = 32),
    delta_json         BLOB,
    delta_sha256       BLOB,
    operation_count    INTEGER NOT NULL DEFAULT 0 CHECK (operation_count >= 0),
    PRIMARY KEY (session_id, event_uid),
    UNIQUE (session_id, run_id, seq),
    FOREIGN KEY (session_id, event_uid)
        REFERENCES records(session_id, event_uid) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS records_by_run ON records (session_id, run_id, seq);
CREATE INDEX IF NOT EXISTS records_by_time ON records (session_id, recorded_at, seq);
CREATE INDEX IF NOT EXISTS representations_by_base ON representations (session_id, run_id, base_seq);
`

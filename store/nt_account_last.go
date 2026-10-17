// F35 — the AddOn's last accounts_list frame, persisted so the installation
// gate's sim_accounts leg can vouch for the bound accounts after a bot restart
// with NT8 closed. The frame is the AddOn's own account discovery (name + the
// SIM flag NT8's Account.Simulation reports, plus the connection type name when
// the AddOn could read one).
//
// One row per account NAME (the stable key): every frame UPSERTs the accounts it
// carries, so a name keeps its latest state and a name that leaves the list
// keeps its last-known row — never deleted (THE ONE RULE). A non-SIM row stays
// non-SIM; missing data reads fail-closed at the gate.
package store

import (
	"time"

	"gorm.io/gorm"
)

// NTAccountLast is one account's last-known state.
type NTAccountLast struct {
	Name       string `gorm:"column:name;primaryKey" json:"name"`
	IsSim      bool   `gorm:"column:is_sim" json:"is_sim"`
	Connection string `gorm:"column:connection" json:"connection"`
	// ReceivedAtMs is the wall-clock (UTC ms) the bot stored the frame. It is
	// the "last seen" the gate quotes.
	ReceivedAtMs int64 `gorm:"column:received_at_ms;index" json:"received_at_ms"`
}

func (NTAccountLast) TableName() string { return "nt_accounts_last" }

// NTAccountLastStore wraps the gorm handle.
type NTAccountLastStore struct{ db *gorm.DB }

func NewNTAccountLastStore(db *gorm.DB) *NTAccountLastStore {
	return &NTAccountLastStore{db: db}
}

func (s *NTAccountLastStore) Migrate() error {
	return s.db.AutoMigrate(&NTAccountLast{})
}

// Upsert writes each account in one frame. INSERT-or-UPDATE on the name key —
// no deletes, so an account that drops off the frame keeps its last row.
func (s *NTAccountLastStore) Upsert(accounts []NTAccountLast) error {
	if len(accounts) == 0 {
		return nil
	}
	for _, a := range accounts {
		if err := s.db.Exec(
			`INSERT INTO nt_accounts_last (name, is_sim, connection, received_at_ms)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET
			   is_sim = excluded.is_sim,
			   connection = excluded.connection,
			   received_at_ms = excluded.received_at_ms`,
			a.Name, a.IsSim, a.Connection, a.ReceivedAtMs,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

// List returns the last persisted account list, every account name ever seen at
// its latest state, sorted by name. An empty table returns an empty (non-nil)
// slice.
func (s *NTAccountLastStore) List() ([]NTAccountLast, error) {
	var rows []NTAccountLast
	if err := s.db.Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []NTAccountLast{}
	}
	return rows, nil
}

// Get returns one account's last-known row, or nil when never seen.
func (s *NTAccountLastStore) Get(name string) (*NTAccountLast, error) {
	var r NTAccountLast
	err := s.db.Where("name = ?", name).First(&r).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// LatestReceivedAt returns the newest received_at_ms across every row, or
// ok=false when the table is empty.
func (s *NTAccountLastStore) LatestReceivedAt() (int64, bool) {
	var max int64
	row := s.db.Raw(`SELECT COALESCE(MAX(received_at_ms), 0) FROM nt_accounts_last`).Row()
	if row == nil {
		return 0, false
	}
	if err := row.Scan(&max); err != nil || max == 0 {
		return 0, false
	}
	return max, true
}

// LastSeenCT formats a received_at_ms as "hh:mm" CT (the owner's wall clock),
// the same "last seen" the gate detail prints.
func LastSeenCT(receivedAtMs int64) string {
	if receivedAtMs <= 0 {
		return "never"
	}
	// CT is the canonical wall clock everywhere (owner rule).
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.UnixMilli(receivedAtMs).UTC().Format("15:04") + " UTC"
	}
	return time.UnixMilli(receivedAtMs).In(loc).Format("15:04") + " CT"
}

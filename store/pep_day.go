package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ── PEP MODE — the durable one-trade-per-day record (owner order 2026-10-29) ──
//
// One row per pep ENTRY the bot has decided to send, keyed (trader, CT day,
// slot). The row is written BEFORE the order is sent and read after any
// restart, so a restart, a reload (a Studio save removes and re-adds every bound
// trader) or a crash between the decision and the fill can never fire a second
// entry on the same day. Memory is never the source of truth.
//
// STATES (the lifecycle of one entry):
//
//	claimed   the entry was decided and recorded; the wire has NOT been reached
//	sending   the send is about to reach the wire (written immediately before it)
//	released  the claim was withdrawn because the entry provably never reached
//	          the wire (an admission or pre-wire refusal after the claim)
//
// COUNTING (fail closed): every row EXCEPT released counts toward the day's
// cap. A claim that was never advanced (a crash between claim and send) still
// counts: the day's trade is consumed, never retried on a guess. Only a row the
// bot itself marked released (it saw the refusal happen before the wire) frees
// its slot.
//
// The slot is part of the primary key: two concurrent claims for the same slot
// collide in the database, not in a Go mutex, so no process and no pair of
// goroutines can both win it.
const (
	PepDayClaimed  = "claimed"
	PepDaySending  = "sending"
	PepDayReleased = "released"
)

// ErrPepDayCapReached is returned by ClaimEntry when the day's cap is already
// consumed (the one-trade-per-day rule). It is a normal refusal, not a failure.
var ErrPepDayCapReached = errors.New("pep: the day's trade cap is already consumed")

// PepDayStore persists the pep one-trade-per-day record.
type PepDayStore struct {
	db *gorm.DB
}

// PepDayDB is one pep entry's durable record.
type PepDayDB struct {
	// RowKey is traderID|CT day|slot (1-based). The primary key.
	RowKey    string `gorm:"column:row_key;primaryKey"`
	TraderID  string `gorm:"column:trader_id;not null;default:'';index:idx_pep_days_trader_day,priority:1"`
	Day       string `gorm:"column:day;not null;default:'';index:idx_pep_days_trader_day,priority:2"` // CT date 2006-01-02
	Slot      int    `gorm:"column:slot;not null;default:0"`
	State     string `gorm:"column:state;not null;default:''"`
	Style     string `gorm:"column:style;not null;default:''"`
	Side      string `gorm:"column:side;not null;default:''"` // long | short
	Symbol    string `gorm:"column:symbol;not null;default:''"`
	Contracts int    `gorm:"column:contracts;not null;default:0"`
	// SignalID is the evaluator's signal identity (style | day | side | signal
	// close ms): the same signal re-seen on a later tick maps to the same id.
	SignalID  string  `gorm:"column:signal_id;not null;default:''"`
	RefEntry  float64 `gorm:"column:ref_entry;not null;default:0"` // the evaluator's reference entry
	BoxHigh   float64 `gorm:"column:box_high;not null;default:0"`
	BoxLow    float64 `gorm:"column:box_low;not null;default:0"`
	Stop      float64 `gorm:"column:stop;not null;default:0"` // the wire bracket actually sent
	Target    float64 `gorm:"column:target;not null;default:0"`
	Note      string  `gorm:"column:note;not null;default:''"` // why a claim was released
	ClaimedAt int64   `gorm:"column:claimed_at;not null;default:0"`
	UpdatedAt int64   `gorm:"column:updated_at;not null;default:0"`
}

// TableName implements the gorm Tabler interface.
func (PepDayDB) TableName() string { return "pep_days" }

// NewPepDayStore creates a PepDayStore.
func NewPepDayStore(db *gorm.DB) *PepDayStore { return &PepDayStore{db: db} }

func (s *PepDayStore) initTables() error {
	return s.db.AutoMigrate(&PepDayDB{})
}

// PepDayKey builds the primary key of one entry slot.
func PepDayKey(traderID, day string, slot int) string {
	return fmt.Sprintf("%s|%s|%d", traderID, day, slot)
}

// PepDayState is what the trader reads at every evaluation: how many of the
// day's entries are consumed, and the newest row.
type PepDayState struct {
	Day     string
	Counted int // rows that count toward the cap (every state but released)
	Last    *PepDayDB
}

// Day reads the day's state. A read error is returned; the caller fails closed.
func (s *PepDayStore) Day(traderID, day string) (PepDayState, error) {
	if s == nil || s.db == nil {
		return PepDayState{}, errors.New("pep day store is not available")
	}
	traderID, day = strings.TrimSpace(traderID), strings.TrimSpace(day)
	if traderID == "" || day == "" {
		return PepDayState{}, errors.New("pep day read needs a trader id and a CT day")
	}
	var rows []PepDayDB
	if err := s.db.Where("trader_id = ? AND day = ?", traderID, day).Order("slot ASC").Find(&rows).Error; err != nil {
		return PepDayState{}, err
	}
	st := PepDayState{Day: day}
	for i := range rows {
		if rows[i].State != PepDayReleased {
			st.Counted++
		}
		r := rows[i]
		st.Last = &r
	}
	return st, nil
}

// ClaimEntry records an entry BEFORE it is sent. It returns the claimed row, or
// ErrPepDayCapReached when maxTrades entries are already counted for the day,
// or any database error (the caller fails closed: no row, no send).
//
// The slot is the first free one (1..maxTrades): a released row's slot is reused
// by writing over THAT row only when it is released; a counted row is never
// touched. The insert itself is the arbiter - a concurrent claim for the same
// slot fails on the primary key.
func (s *PepDayStore) ClaimEntry(row PepDayDB, maxTrades int, nowMs int64) (*PepDayDB, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("pep day store is not available")
	}
	if strings.TrimSpace(row.TraderID) == "" || strings.TrimSpace(row.Day) == "" {
		return nil, errors.New("pep claim needs a trader id and a CT day")
	}
	if maxTrades < 1 {
		return nil, errors.New("pep claim needs a cap of at least 1")
	}
	var out *PepDayDB
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []PepDayDB
		if err := tx.Where("trader_id = ? AND day = ?", row.TraderID, row.Day).Order("slot ASC").Find(&rows).Error; err != nil {
			return err
		}
		counted := 0
		bySlot := map[int]PepDayDB{}
		for _, r := range rows {
			bySlot[r.Slot] = r
			if r.State != PepDayReleased {
				counted++
			}
		}
		if counted >= maxTrades {
			return ErrPepDayCapReached
		}
		slot := 0
		for i := 1; i <= maxTrades+len(rows); i++ { // a free or released slot
			if r, ok := bySlot[i]; !ok || r.State == PepDayReleased {
				slot = i
				break
			}
		}
		if slot == 0 {
			return ErrPepDayCapReached
		}
		row.Slot = slot
		row.RowKey = PepDayKey(row.TraderID, row.Day, slot)
		row.State = PepDayClaimed
		row.ClaimedAt, row.UpdatedAt = nowMs, nowMs
		if _, ok := bySlot[slot]; ok {
			// a released slot: replace THAT row (its claim was withdrawn).
			res := tx.Model(&PepDayDB{}).Where("row_key = ? AND state = ?", row.RowKey, PepDayReleased).
				Select("*").Updates(&row)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("pep claim: slot %d changed under the claim (fail closed)", slot)
			}
		} else if err := tx.Create(&row).Error; err != nil {
			return err
		}
		c := row
		out = &c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkSending advances claimed -> sending, immediately before the wire. A row
// that is not in the claimed state is refused (fail closed). stop/target are the
// wire bracket that is about to be sent.
func (s *PepDayStore) MarkSending(rowKey string, stop, target float64, nowMs int64) error {
	if s == nil || s.db == nil {
		return errors.New("pep day store is not available")
	}
	res := s.db.Model(&PepDayDB{}).Where("row_key = ? AND state = ?", rowKey, PepDayClaimed).
		Updates(map[string]any{"state": PepDaySending, "stop": stop, "target": target, "updated_at": nowMs})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("pep day %s is not in the claimed state (fail closed)", rowKey)
	}
	return nil
}

// Release withdraws a claim that provably never reached the wire. ONLY a row
// still in the claimed state can be released; a sending row never can.
func (s *PepDayStore) Release(rowKey, note string, nowMs int64) error {
	if s == nil || s.db == nil {
		return errors.New("pep day store is not available")
	}
	res := s.db.Model(&PepDayDB{}).Where("row_key = ? AND state = ?", rowKey, PepDayClaimed).
		Updates(map[string]any{"state": PepDayReleased, "note": note, "updated_at": nowMs})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("pep day %s is not in the claimed state: a sent entry is never released", rowKey)
	}
	return nil
}

// PepDay gets the pep one-trade-per-day storage.
func (s *Store) PepDay() *PepDayStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pepDay == nil {
		s.pepDay = NewPepDayStore(s.gdb)
	}
	return s.pepDay
}

// PepDayOf is the CT calendar day of an instant (the day the cap belongs to);
// "" when America/Chicago is unavailable (the caller fails closed).
func PepDayOf(t time.Time) string {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return ""
	}
	return t.In(loc).Format("2006-01-02")
}

package store

import (
	"fmt"
	"sort"
	"strings"

	"vl/logger"
	"vl/telemetry"
)

// OWNER IDENTITY (lane owner-identity, CTO dispatch 2026-10-14). ONE resolver
// answers "which account owns this box" for every owner check: the owner-only
// API routes (api requireOwner) and the Telegram bot's identity
// (telegram botIdentity.refresh). Both used to take users[0] — the
// first-created account — on the comment's promise that "registration closes
// after the first user". On the live box that account is a TEST account that
// owns no trader, while every trader belongs to the second account, so the
// owner-only route trusted the wrong account and the bot minted its JWT for
// an account that owns nothing.
//
// The rule, fail-closed, in order:
//
//  1. An explicitly configured owner wins. NONE EXISTS: no config field or env
//     knob names an owner (grep of config/, .env.example, main.go at the base),
//     and this wave adds none the owner would have to set. The updater's
//     enrollment (data/updater/admin.json) is the /updates admin, guarded by its
//     own gate; it is deliberately NOT read here (a password change un-enrolls
//     it, and an unenrolled box is its normal OFF state — neither may change
//     who owns the box).
//  2. Every trader row shares ONE user_id and that user exists → that user
//     (source "traders").
//  3. There are NO traders → the first-created user (source "first-user (no
//     traders)") — the old rule, kept only where nothing better exists.
//  4. Anything else — traders split across users, the traders' user_id
//     missing from users, no users at all, or a store read error → NO owner:
//     owner-only routes refuse and the Telegram bot acts for nobody. Each
//     refusal is logged and counted (RecordOwnerRefusal, gate
//     "owner_unresolved").
//
// The resolver only READS users and traders. It never writes either table.

// Owner sources (Owner.Source).
const (
	OwnerSourceTraders   = "traders"
	OwnerSourceFirstUser = "first-user (no traders)"
)

// OwnerReasonNoUsers is Owner.Reason on a box where nobody has registered —
// the one unresolved case whose remedy is registration, not a fix.
const OwnerReasonNoUsers = "no users and no traders (nobody has registered)"

// OwnerUnresolvedGate is the gate-block counter (GET /api/risk/gate-blocks,
// trader "") one refusal caused by an unresolved owner increments.
const OwnerUnresolvedGate = "owner_unresolved"

// Owner is the box owner as ResolveOwner read it. UserID == "" means NO
// owner: Reason says why, User is nil.
type Owner struct {
	UserID string
	// User is the row the owner resolved to (nil when unresolved) — the bot
	// mints from it, so the user and its credential epoch come from the same
	// read as the decision.
	User         *User
	Source       string // OwnerSourceTraders | OwnerSourceFirstUser; "" when unresolved
	Reason       string // why there is no owner; "" when resolved
	OwnedTraders int    // trader rows whose user_id is UserID
	TotalTraders int    // every trader row; -1 when the traders could not be read
}

// Resolved reports whether an owner was found.
func (o Owner) Resolved() bool { return o.UserID != "" }

// ownerIDPrefix is the printable form of a user id: its first 8 characters.
// Ids are not secrets, but a boot line never needs more; emails are never
// printed.
func ownerIDPrefix(id string) string {
	if id == "" {
		return "(empty)"
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// BootLine is the 👤 owner line, read from the resolution (never typed).
func (o Owner) BootLine() string {
	if !o.Resolved() {
		return fmt.Sprintf("👤 owner: NONE — %s; owner-only routes refuse, the Telegram bot is off", o.Reason)
	}
	return fmt.Sprintf("👤 owner: user %s (source %s · owns %d of %d traders)",
		ownerIDPrefix(o.UserID), o.Source, o.OwnedTraders, o.TotalTraders)
}

// ResolveOwner applies the owner rule to the users and traders as they stand
// now. A read error resolves to NO owner (fail closed) with the error as the
// reason.
func (s *Store) ResolveOwner() Owner {
	users, err := s.User().GetAll()
	if err != nil {
		return Owner{Reason: fmt.Sprintf("users unreadable (%v)", err), TotalTraders: -1}
	}
	traders, err := s.Trader().ListAll()
	if err != nil {
		return Owner{Reason: fmt.Sprintf("traders unreadable (%v)", err), TotalTraders: -1}
	}
	return resolveOwner(users, traders)
}

// resolveOwner is the rule over one read of both tables. users is ordered by
// created_at (UserStore.GetAll).
func resolveOwner(users []User, traders []*Trader) Owner {
	total := len(traders)
	if total == 0 {
		if len(users) == 0 {
			return Owner{Reason: OwnerReasonNoUsers}
		}
		first := users[0]
		if first.ID == "" {
			return Owner{Reason: "the first-created user has an empty id"}
		}
		return Owner{UserID: first.ID, User: &first, Source: OwnerSourceFirstUser}
	}
	perUser := map[string]int{}
	for _, t := range traders {
		if t == nil {
			continue
		}
		perUser[t.UserID]++
	}
	if len(perUser) != 1 {
		parts := make([]string, 0, len(perUser))
		for id, n := range perUser {
			parts = append(parts, fmt.Sprintf("%s ×%d", ownerIDPrefix(id), n))
		}
		sort.Strings(parts)
		return Owner{
			Reason:       fmt.Sprintf("the %d traders belong to %d users (%s)", total, len(perUser), strings.Join(parts, ", ")),
			TotalTraders: total,
		}
	}
	var only string
	for id := range perUser {
		only = id
	}
	for i := range users {
		if users[i].ID == only && only != "" {
			u := users[i]
			return Owner{UserID: only, User: &u, Source: OwnerSourceTraders, OwnedTraders: perUser[only], TotalTraders: total}
		}
	}
	return Owner{
		Reason:       fmt.Sprintf("the %d traders belong to user %s, which is not in users", total, ownerIDPrefix(only)),
		TotalTraders: total,
	}
}

// RecordOwnerRefusal logs (WARN) and counts one refusal the unresolved owner
// caused at site. Every caller that refuses because ResolveOwner found no
// owner calls it, so no such refusal is silent.
func RecordOwnerRefusal(site string, o Owner) {
	telemetry.IncGateBlock("", OwnerUnresolvedGate)
	logger.Warnf("👤 owner: NONE — %s; refused: %s (owner-only routes refuse, the Telegram bot is off)", o.Reason, site)
}

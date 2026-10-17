package telegram

// PR #200 review F7 / #22 (CTO bridge msg 1790252194343): a JWT's iat is
// whole seconds, and the API refuses a token issued at or before the
// credential epoch's second (auth.RetiredBy) — so a bot re-mint in the SAME
// second as the owner's password change is retired at birth. refresh()
// installed it and reported success, and that message's agent calls 401'd.
// The same shape after F4b: a re-mint in the same second as the blacklisted
// token's own mint is byte-identical to it (whole-second iat/nbf/exp, no
// jti), so it is still on the blacklist.
//
// The fold: after minting, refresh re-checks the fresh token with the
// predicate that sent it there (botTokenStale — retired, blacklisted, or
// otherwise refused); if it would be refused, it waits to the next whole
// second (botSleep(botUntilNextSecond(now))) and mints ONCE more; still
// refused → false, and nothing is installed. refresh never reports success
// with a token the API refuses.
//
// Pinned at refresh — the call site runBot reaches at start, on /start and
// before every AI call (TestRunBotMintsOnlyThroughRefresh) — against the
// PRODUCTION server (api.NewServer + Start, real authMiddleware), with the
// row's updated_at written in the CURRENT second by the production writer
// the password-change handler calls (store.UserStore.UpdatePassword).
//
// The mint's own clock is real and cannot be seamed (auth
// TestServerNeverMintsAFutureIat: every iat is jwt.NewNumericDate(time.Now())),
// so the same-second path depends on the wall clock not crossing a second
// boundary between the stamp and the mint (a millisecond or so): each pin
// retries that setup, and asserts the refresh invariant on EVERY attempt.

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"vl/auth"
	"vl/store"
	"vl/telegram/agent"
)

// btWaits records refresh's waits. real=false: nothing sleeps (the second
// mint lands in the same second as the first); real=true: the production
// wait runs.
type btWaits struct {
	d  []time.Duration
	at []time.Time
}

func btRecordWaits(t *testing.T, real bool) *btWaits {
	t.Helper()
	w := &btWaits{}
	prev := botSleep
	botSleep = func(d time.Duration) {
		w.d = append(w.d, d)
		w.at = append(w.at, time.Now())
		if real {
			prev(d)
		}
	}
	t.Cleanup(func() { botSleep = prev })
	return w
}

func (w *btWaits) reset() { w.d, w.at = nil, nil }

// btNeverSucceedsRefused is the refresh invariant: success means the bot
// holds a token the production router admits.
func btNeverSucceedsRefused(t *testing.T, base string, ok bool, ident *botIdentity) {
	t.Helper()
	if !ok {
		return
	}
	if code, body := btCall(t, base, "GET", "/api/my-traders", ident.token, ""); code != http.StatusOK {
		t.Fatalf("refresh reported success with a token the API refuses: GET /api/my-traders = %d %s — that message's agent calls 401", code, body)
	}
}

// btWaitReachesPast: the one wait refresh asked for is at most a second (plus
// the margin) and ends past the given whole second.
func btWaitReachesPast(t *testing.T, w *btWaits, sec int64) {
	t.Helper()
	d := w.d[0]
	if d <= 0 || d > time.Second+botNextSecondMargin {
		t.Fatalf("refresh asked to wait %v — want (0, 1s+%v]", d, botNextSecondMargin)
	}
	if end := w.at[0].Add(d); end.Unix() <= sec {
		t.Fatalf("refresh's wait ends at %v — still in second %d, where the re-mint is refused again", end, sec)
	}
}

const btSameSecondAttempts = 5

// btEarlierSecondBotToken is a bot token for the owner stamped 5 s ago
// (iat = nbf), signed with the test's secret — so the token refresh holds
// BEFORE a same-second re-mint can never be byte-identical to that re-mint,
// and the pin's "nothing installed" token check can fire (F7 verify note 1:
// with a starting token from the same second, a mutant installing the
// refused re-mint passed every pin).
func btEarlierSecondBotToken(t *testing.T) string {
	t.Helper()
	at := jwt.NewNumericDate(time.Now().Add(-5 * time.Second))
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: btOwnerID, Email: auth.BotInternalEmail, Scope: auth.ScopeTelegram,
		RegisteredClaims: jwt.RegisteredClaims{IssuedAt: at, NotBefore: at, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), Issuer: "nofxAI"},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// No real sleep: the second mint lands in the same second, so it is refused
// too — refresh must stop after ONE wait and report false, installing nothing.
func TestBotRefreshSameSecondReMintIsBoundedAndFailsClosedAtItsCallSite(t *testing.T) {
	base, st := btBoot(t)
	btPrivateSecret(t)
	hash, err := auth.HashPassword("owner-rotated-pass-f7")
	if err != nil {
		t.Fatal(err)
	}
	ident := newBotIdentity(st, 0)
	if !ident.refresh() {
		t.Fatal("refresh with an account on the box = false")
	}
	w := btRecordWaits(t, false)
	for attempt := 1; ; attempt++ {
		if attempt > btSameSecondAttempts {
			t.Fatalf("the same-second path was never reached in %d attempts (the wall clock crossed a second between the stamp and a mint every time)", btSameSecondAttempts)
		}
		ident.token = btEarlierSecondBotToken(t) // never byte-identical to a re-mint in the epoch's second
		before, beforeAgents := ident.token, ident.agents
		// The owner's password change, in the CURRENT second.
		if err := st.User().UpdatePassword(btOwnerID, hash); err != nil {
			t.Fatal(err)
		}
		epoch := btRow(t, st).UpdatedAt.Unix()
		w.reset()
		ok := ident.refresh()
		btNeverSucceedsRefused(t, base, ok, ident)
		if len(w.d) > 1 {
			t.Fatalf("refresh waited %d times — the re-mint must be bounded to ONE extra mint", len(w.d))
		}
		if len(w.d) == 0 && !ok {
			t.Fatal("refresh failed closed without its one wait to the next second")
		}
		if len(w.d) == 0 || ok {
			continue // a mint landed past the epoch's second on its own: admitted (checked above); retry
		}
		// Both mints in the epoch's second: refused twice → false.
		btWaitReachesPast(t, w, epoch)
		if ident.token != before || ident.agents != beforeAgents {
			t.Fatal("refresh failed but installed a new token / manager — a token the API refuses must never be installed")
		}
		return
	}
}

// The one wait, then the admitted re-mint — DETERMINISTIC (CTO
// 1790255882118: the real-sleep version failed once under the full -race
// gate, "refresh = false after waiting to the next second", a real-clock
// second-boundary race: this box's chrony steps the clock back ~1 s every
// ~2 min). No real sleep, and no dependence on which second the machine is
// in. The mint's iat is time.Now() by design (auth pins it), so the test
// moves the OTHER side of the comparison — the credential epoch, which it
// owns:
//   - the epoch starts 30 s AHEAD of the clock, so the first mint is retired
//     at birth (the same-second case, whatever the second);
//   - the sleep seam stands in for the wall clock reaching the next second:
//     instead of sleeping, it moves the epoch 2 s into the PAST — the
//     relation the real wait produces (the re-mint's iat is past the
//     epoch's second);
//   - the clock seam fixes "now", so the wait refresh asks for is exactly
//     botUntilNextSecond(botNow()).
func TestBotRefreshSameSecondReMintWaitsForTheNextSecondAtItsCallSite(t *testing.T) {
	base, st := btBoot(t)
	btPrivateSecret(t)
	ident := newBotIdentity(st, 0)
	if !ident.refresh() {
		t.Fatal("refresh with an account on the box = false")
	}
	ident.token = btEarlierSecondBotToken(t) // retired: stamped before the epoch
	before, beforeAgents := ident.token, ident.agents

	// One fake clock drives the wait (botNow) AND the mint's iat (botMint);
	// the sleep seam advances it. It starts 300 ms into the real current
	// second S, so the tokens it stamps are within the parser's leeway.
	cur := time.Now().Truncate(time.Second).Add(300 * time.Millisecond)
	prevNow, prevMint, prevSleep := botNow, botMint, botSleep
	t.Cleanup(func() { botNow, botMint, botSleep = prevNow, prevMint, prevSleep })
	botNow = func() time.Time { return cur }
	botMint = btMintStampedBy(func() time.Time { return cur })
	var waits []time.Duration
	botSleep = func(d time.Duration) { waits = append(waits, d); cur = cur.Add(d) }
	// The owner's password change in second S: a mint stamped in S is
	// retired at birth; one stamped in S+1 is admitted.
	btSetOwnerUpdatedAt(t, st, cur.Truncate(time.Second).Add(100*time.Millisecond))

	ok := ident.refresh()
	if len(waits) != 1 {
		t.Fatalf("refresh waited %d times — want exactly ONE wait before its one extra mint", len(waits))
	}
	if want := 700*time.Millisecond + botNextSecondMargin; waits[0] != want {
		t.Fatalf("refresh asked to wait %v — want botUntilNextSecond(botNow()) = %v (the wait must come from the clock seam)", waits[0], want)
	}
	if !ok {
		t.Fatal("refresh = false after its one wait — the re-mint in the next second is admitted")
	}
	if ident.token == before || ident.agents == beforeAgents {
		t.Fatal("refresh succeeded without re-minting / rebuilding the manager on the retired token")
	}
	if code, body := btCall(t, base, "GET", "/api/my-traders", ident.token, ""); code != http.StatusOK {
		t.Fatalf("the re-minted token on GET /api/my-traders = %d %s — want 200", code, body)
	}
	if code, _ := btCall(t, base, "GET", "/api/my-traders", before, ""); code != http.StatusUnauthorized {
		t.Fatalf("control: the retired token on GET /api/my-traders = %d — want 401", code)
	}
}

// btMintStampedBy mints a bot token exactly as agent.GenerateBotToken does,
// but with iat = nbf = the given clock (whole seconds) and exp = iat + 24h.
func btMintStampedBy(now func() time.Time) func(string) (string, error) {
	return func(userID string) (string, error) {
		at := jwt.NewNumericDate(now())
		return jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
			UserID: userID, Email: auth.BotInternalEmail, Scope: auth.ScopeTelegram,
			RegisteredClaims: jwt.RegisteredClaims{IssuedAt: at, NotBefore: at, ExpiresAt: jwt.NewNumericDate(now().Add(24 * time.Hour)), Issuer: "nofxAI"},
		}).SignedString(auth.JWTSecret)
	}
}

// btSetOwnerUpdatedAt moves the owner row's credential epoch (users.updated_at)
// directly — the test owns the epoch so no pin depends on the machine's
// second.
func btSetOwnerUpdatedAt(t *testing.T, st *store.Store, at time.Time) {
	t.Helper()
	if err := st.GormDB().Exec(`UPDATE users SET updated_at = ? WHERE id = ?`, at.UTC(), btOwnerID).Error; err != nil {
		t.Fatal(err)
	}
}

// The seams are the real clock and the real minter in production (a pin, so
// a test-only default can never ship): botNow is time.Now, botSleep is
// time.Sleep, botMint is agent.GenerateBotToken.
func TestBotClockSeamsAreTheRealClockInProduction(t *testing.T) {
	if reflect.ValueOf(botNow).Pointer() != reflect.ValueOf(time.Now).Pointer() {
		t.Fatal("botNow is not time.Now in production")
	}
	if reflect.ValueOf(botSleep).Pointer() != reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatal("botSleep is not time.Sleep in production")
	}
	if reflect.ValueOf(botMint).Pointer() != reflect.ValueOf(agent.GenerateBotToken).Pointer() {
		t.Fatal("botMint is not agent.GenerateBotToken in production")
	}
}

// F4b × F7, the jti world (token-iat-same-second, owner order 2026-10-02):
// every mint carries a distinct jti, so a re-mint in the same second as a
// blacklisted token is a NEW string and refresh succeeds with zero waits —
// the old byte-identical re-mint can no longer exist. The bounded-refusal
// invariant (one wait, one extra mint, never install a refused token) is
// unchanged and is re-asserted here through the retire path, which is still
// what refuses a re-mint deterministically (a frozen mint clock keeps both
// mints in the credential epoch's second).
func TestBotRefreshSameSecondReMintAfterABlacklistIsBoundedAtItsCallSite(t *testing.T) {
	base, st := btBoot(t)
	btPrivateSecret(t)
	ident := newBotIdentity(st, 0)
	if !ident.refresh() {
		t.Fatal("refresh with an account on the box = false")
	}

	// The mint clock is frozen into the current whole second, so the re-mint
	// lands in the blacklisted token's own second deterministically (the
	// auth.Now seam — never a sleep).
	cur := time.Now().Truncate(time.Second).Add(300 * time.Millisecond)
	prevAuthNow := auth.Now
	auth.Now = func() time.Time { return cur }
	t.Cleanup(func() { auth.Now = prevAuthNow })

	// Phase A — blacklisted token: the same-second re-mint is a distinct
	// string (jti) and is admitted immediately, with no wait.
	before, beforeAgents := ident.token, ident.agents
	cl, err := auth.ValidateJWT(before)
	if err != nil || cl.ExpiresAt == nil || cl.IssuedAt == nil {
		t.Fatalf("the bot's token does not parse to iat/exp: %v", err)
	}
	auth.BlacklistToken(before, cl.ExpiresAt.Time)
	w := btRecordWaits(t, false)
	ok := ident.refresh()
	btNeverSucceedsRefused(t, base, ok, ident)
	if !ok {
		t.Fatal("refresh = false after the blacklist — the same-second re-mint is a NEW string (jti) and must be admitted")
	}
	if len(w.d) != 0 {
		t.Fatalf("refresh waited %d times — a distinct re-mint needs no wait", len(w.d))
	}
	if ident.token == before || ident.agents == beforeAgents {
		t.Fatal("refresh reported success without re-minting / rebuilding the manager")
	}
	if code, body := btCall(t, base, "GET", "/api/my-traders", ident.token, ""); code != http.StatusOK {
		t.Fatalf("the re-minted token on GET /api/my-traders = %d %s — want 200", code, body)
	}
	if code, _ := btCall(t, base, "GET", "/api/my-traders", before, ""); code != http.StatusUnauthorized {
		t.Fatalf("control: the blacklisted token on GET /api/my-traders = %d — want 401", code)
	}

	// Phase B — the bound the old test asserted: a re-mint that IS still
	// refused (the credential epoch's second, not the blacklist) must wait
	// ONCE, mint ONE more time, and install nothing.
	btSetOwnerUpdatedAt(t, st, cur.Truncate(time.Second).Add(100*time.Millisecond))
	// ident.token is phase A's re-mint, stamped in the frozen current second
	// — retired by the epoch stamped in that same second.
	before, beforeAgents = ident.token, ident.agents
	w.reset()
	ok = ident.refresh()
	btNeverSucceedsRefused(t, base, ok, ident)
	if len(w.d) > 1 {
		t.Fatalf("refresh waited %d times — the re-mint must be bounded to ONE extra mint", len(w.d))
	}
	if len(w.d) == 0 {
		t.Fatal("refresh failed closed without its one wait to the next second")
	}
	if ok {
		t.Fatal("refresh reported success although every mint in the frozen epoch's second is retired")
	}
	btWaitReachesPast(t, w, cur.Truncate(time.Second).Unix())
	if ident.token != before || ident.agents != beforeAgents {
		t.Fatal("refresh failed but installed a new token / manager — a token the API refuses must never be installed")
	}
}

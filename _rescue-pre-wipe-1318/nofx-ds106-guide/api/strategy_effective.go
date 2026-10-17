// W-EXEC-TRUTH W1 (g) — GET /api/strategies/:id/effective?session=<S>
//
// Every settings row with its EFFECTIVE value, its ORIGIN and its SCOPE, read
// from the STORED strategy row (trader.EffectiveSettings). Deliberately NOT from
// handleGetStrategy's payload: attachPublishConfig runs ClampLimits on that
// copy, so an unset min_risk_reward_ratio arrives there as a plausible 3.0 with
// no trace that nobody saved it.
//
// Ownership is the strategy GET's own: Strategy().Get(userID, id) — another
// user's id is 404, never 403. Secrets (the NofxOS key, external data-source
// headers/URLs, any credential-shaped leaf) are redacted in the stored value
// AND the effective value.

package api

import (
	"net/http"
	"regexp"
	"strings"

	"vl/trader"

	"github.com/gin-gonic/gin"
)

var effectiveSessionRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)

// canonicalEffectiveSession is the one canonicaliser for the ?session= value
// (class 28: canonicalise where the value ENTERS). "" means no session named.
func canonicalEffectiveSession(raw string) (string, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return "", true
	}
	return s, effectiveSessionRe.MatchString(s)
}

// effectiveVenue is the exchange type the strategy trades on: ?venue= when
// given; else the one exchange type of this user's traders bound to the
// strategy; else, with no binding (or more than one type), the process's
// trading mode (futures → ninjatrader). "" = unknown, and the rows that depend
// on the venue say n/a rather than guess.
func (s *Server) effectiveVenue(c *gin.Context, userID, strategyID string) string {
	if v := strings.ToLower(strings.TrimSpace(c.Query("venue"))); v != "" {
		return v
	}
	types := map[string]bool{}
	if traders, err := s.store.Trader().List(userID); err == nil {
		for _, t := range traders {
			if t == nil || t.StrategyID != strategyID || t.ExchangeID == "" {
				continue
			}
			if ex, err := s.store.Exchange().GetByID(userID, t.ExchangeID); err == nil && ex != nil && ex.ExchangeType != "" {
				types[strings.ToLower(ex.ExchangeType)] = true
			}
		}
	}
	if len(types) == 1 {
		for t := range types {
			return t
		}
	}
	// C2 — futures-only build: the default venue is always ninjatrader.
	return "ninjatrader"
}

func (s *Server) handleStrategyEffective(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	st, err := s.store.Strategy().Get(userID, c.Param("id"))
	if err != nil || st == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	session, ok := canonicalEffectiveSession(c.Query("session"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session must be a session name such as NY, ASIA or LONDON"})
		return
	}
	venue := s.effectiveVenue(c, userID, st.ID)

	rows, err := trader.EffectiveSettings(st.Config, venue, session, s.store.Strategy().ExplicitZeroRecordOf(st.ID))
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "stored strategy config does not parse"})
		return
	}

	// An unnamed session / unknown venue is null — absent, not "".
	var sessionOut, venueOut any
	if session != "" {
		sessionOut = session
	}
	if venue != "" {
		venueOut = venue
	}
	c.JSON(http.StatusOK, gin.H{
		"strategy_id": st.ID,
		"session":     sessionOut,
		"venue":       venueOut,
		"settings":    rows,
		"coverage":    trader.EffectiveCoverageOf(rows),
	})
}

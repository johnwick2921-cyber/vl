// GOLDEN: the exact JSON bytes the trader lane's GET /api/traders/:id/pep-truth
// emits (fix/m458-pep-trader: trader/pep_truth.go `PepTruth`, encoded by
// encoding/json). Written from that struct's tags - non-omitempty fields are
// ALWAYS present (box, trades_today, position_open, flat_countdown_sec), the
// omitempty ones only when set, `signal` a plain string word, and NO `symbol`.
// The UI tests feed THESE strings (not a UI-shaped object) through the card,
// the chart-line builder and the mode chip, so a renamed or reshaped field is
// RED here instead of shipping a dead card (review round 1, defect 1).

/** pep ON, style E, evaluated, waiting for the EMA cross; flat in 3h05m. */
export const GOLDEN_WAITING_SIGNAL = `{"enabled":true,"mode":"pep","style":"E","contracts":3,"day":"2026-10-30","box":{"high":25100.5,"low":25020.25,"size":80.25,"drawn":true},"signal":"waiting_signal","trades_today":0,"max_trades_per_day":1,"position_open":false,"flat_time_ct":"15:00","flat_countdown_sec":11100,"last_evaluated_ms":1793369225000}`

/** pep ON, a long is open with its bracket. */
export const GOLDEN_POSITION_OPEN = `{"enabled":true,"mode":"pep","style":"A","contracts":3,"day":"2026-10-30","box":{"high":25100.5,"low":25020.25,"size":80.25,"drawn":true},"signal":"position_open","trades_today":1,"max_trades_per_day":1,"position_open":true,"stop":25020.25,"target":25180.75,"side":"long","flat_time_ct":"15:00","flat_countdown_sec":7200,"last_evaluated_ms":1793369225000}`

/** pep ON but never evaluated since the process started: zero box, no as-of. */
export const GOLDEN_NO_DATA = `{"enabled":true,"mode":"pep","style":"A","contracts":3,"day":"2026-10-30","box":{"high":0,"low":0,"size":0,"drawn":false},"signal":"no_data","trades_today":0,"max_trades_per_day":1,"position_open":false,"flat_time_ct":"15:00","flat_countdown_sec":11100}`

/** pep ON, a block refusal, flat time reached. */
export const GOLDEN_AFTER_FLAT = `{"enabled":true,"mode":"pep","style":"E","contracts":3,"day":"2026-10-30","box":{"high":25100.5,"low":25020.25,"size":80.25,"drawn":true},"signal":"after_flat","refusals":[{"code":"pep_after_flat_time","severity":"block","detail":"15:00 CT passed"}],"trades_today":1,"max_trades_per_day":1,"position_open":false,"flat_time_ct":"15:00","flat_countdown_sec":0,"last_evaluated_ms":1793369225000}`

/** pep OFF on an AI trader. */
export const GOLDEN_OFF = `{"enabled":false,"mode":"ai","box":{"high":0,"low":0,"size":0,"drawn":false},"trades_today":0,"position_open":false,"flat_countdown_sec":0}`

/** Mentor AND pep saved ON: the trader was refused at start. */
export const GOLDEN_CONFLICT = `{"enabled":false,"mode":"conflict","conflict":{"code":"pep_mode_conflict_mentor","message":"Pep and mentor mode are both ON."},"box":{"high":0,"low":0,"size":0,"drawn":false},"trades_today":0,"position_open":false,"flat_countdown_sec":0}`

/** Pep AND the Day Plan saved ON: the trader was refused at start. */
export const GOLDEN_CONFLICT_DAY_PLAN = `{"enabled":false,"mode":"conflict","conflict":{"code":"pep_mode_conflict_day_plan","message":"pep mode and the day plan are both ON in this strategy"},"box":{"high":0,"low":0,"size":0,"drawn":false},"trades_today":0,"position_open":false,"flat_countdown_sec":0}`

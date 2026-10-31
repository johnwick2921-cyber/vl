# Event processing clarification, before result publication
TBBO sequence numbers identify venue messages, not unique price rows. A venue
message can contain multiple records. The first integrity pass retained no
results because it conservatively flagged any same-sequence differing prices.
That pass is preserved in run-v1 and is NOT a strategy result.

The adapter now retains all records in source order and marks the final record
of each contiguous (ts_event, sequence) group of the selected contract. EMA and
limit amendments run at that boundary. Existing limits can fill within a group;
new protection becomes eligible in the next group. No instant within-event
reaction is assumed. This is an explicit event-granularity execution model, not
proof of a real queue fill or amendment latency. Price points remain MNQ ticks/4.
Any receive-before-event or event regression still excludes the full session.

Primary schema source: https://databento.com/docs/schemas/tbbo
Event multiplicity: https://databento.com/docs/examples/order-book/order-tracking
No live code or services were changed. Original sources preserved locally in
source-backup-event-fix. Changes have dedicated regression tests.

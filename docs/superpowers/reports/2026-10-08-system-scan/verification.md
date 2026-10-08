# Verification transcripts

These are isolated source-test results, not broker execution receipts. The audit identity probe is intentionally green when it reproduces the defect.

## go-running

```text
ok  	vl	1.102s
ok  	vl/agent	1.484s
ok  	vl/api	21.150s
ok  	vl/auth	0.268s
ok  	vl/branding	48.250s
ok  	vl/calendar	0.003s
?   	vl/cmd/arm-state-sql	[no test files]
?   	vl/cmd/bars-export	[no test files]
?   	vl/cmd/dayplan-arm	[no test files]
?   	vl/cmd/dayplan-level-repair	[no test files]
?   	vl/cmd/dayplan-sessions	[no test files]
?   	vl/cmd/decisive-test	[no test files]
?   	vl/cmd/detector-report	[no test files]
?   	vl/cmd/excursions	[no test files]
ok  	vl/cmd/gate-jwt	7.003s
?   	vl/cmd/gen-updates-check-fixture	[no test files]
?   	vl/cmd/levelstats-backfill	[no test files]
?   	vl/cmd/maintenance-hold	[no test files]
?   	vl/cmd/nq_smoke	[no test files]
?   	vl/cmd/picture_htf_replay	[no test files]
?   	vl/cmd/planner_ab	[no test files]
?   	vl/cmd/planner_replay	[no test files]
?   	vl/cmd/research_export	[no test files]
?   	vl/cmd/sandbox-seed	[no test files]
ok  	vl/cmd/vl-activate	7.230s
ok  	vl/cmd/vl-updater	4.567s
?   	vl/cmd/vl-updater-bootstrap	[no test files]
?   	vl/cmd/w2w3_replay	[no test files]
ok  	vl/config	0.004s
?   	vl/crypto	[no test files]
ok  	vl/deploy	58.054s
ok  	vl/discipline	0.002s
?   	vl/docs/superpowers/reports/2026-09-04-research-conformance-data/d10dump	[no test files]
?   	vl/docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905	[no test files]
ok  	vl/docs/superpowers/reports/2026-09-12-structural-stop/harness	0.009s
?   	vl/docs/superpowers/research/2026-09-12-backtest-structure-fade/harness	[no test files]
?   	vl/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness	[no test files]
ok  	vl/docs/superpowers/research/2026-09-16-round-23/harness	0.007s
ok  	vl/expectancy	0.080s
?   	vl/hook	[no test files]
ok  	vl/internal/activation	1.363s
ok  	vl/internal/censuswalk	1.584s
ok  	vl/internal/holdcli	0.043s
ok  	vl/internal/installpath	0.011s
ok  	vl/internal/retention	1.159s
ok  	vl/internal/retry	1.692s
?   	vl/internal/testhome	[no test files]
ok  	vl/internal/testtmpfs	0.002s
ok  	vl/internal/updateauth	27.664s
ok  	vl/internal/updaterbootstrap	2.309s
ok  	vl/internal/updaterjob	3.845s
ok  	vl/internal/updatersource	0.586s
ok  	vl/internal/updaterwire	0.835s
ok  	vl/internal/updaterwire/wireserver	0.771s
ok  	vl/internal/updaterworker	16.862s
?   	vl/internal/updaterworker/releasefixture	[no test files]
ok  	vl/internal/updaterworker/sqldriverpin	3.410s
?   	vl/internal/updatescheck	[no test files]
ok  	vl/kernel	4.836s
ok  	vl/kernel/mentor	55.689s
?   	vl/levelidentity	[no test files]
ok  	vl/logger	0.005s
?   	vl/manager	[no test files]
ok  	vl/market	0.264s
ok  	vl/mcp	25.068s
ok  	vl/mcp/provider	0.005s
?   	vl/provider/alpaca	[no test files]
ok  	vl/provider/databento	0.125s
ok  	vl/provider/ninjatrader	35.205s
?   	vl/provider/twelvedata	[no test files]
ok  	vl/researchsnapshot	4.942s
ok  	vl/safe	0.005s
?   	vl/security	[no test files]
ok  	vl/store	20.941s
ok  	vl/store/sqlitedriver	0.101s
ok  	vl/telegram	6.630s
ok  	vl/telegram/agent	1.000s
?   	vl/telegram/session	[no test files]
ok  	vl/telemetry	0.552s
ok  	vl/trader	155.038s
ok  	vl/trader/ninjatrader	14.389s
?   	vl/trader/types	[no test files]

```

## go-latest

```text
ok  	vl	3.155s
ok  	vl/agent	1.538s
ok  	vl/api	21.244s
ok  	vl/auth	0.245s
ok  	vl/branding	44.097s
ok  	vl/calendar	0.004s
?   	vl/cmd/arm-state-sql	[no test files]
?   	vl/cmd/bars-export	[no test files]
?   	vl/cmd/dayplan-arm	[no test files]
?   	vl/cmd/dayplan-level-repair	[no test files]
?   	vl/cmd/dayplan-sessions	[no test files]
?   	vl/cmd/decisive-test	[no test files]
?   	vl/cmd/detector-report	[no test files]
?   	vl/cmd/excursions	[no test files]
ok  	vl/cmd/gate-jwt	11.778s
?   	vl/cmd/gen-updates-check-fixture	[no test files]
?   	vl/cmd/levelstats-backfill	[no test files]
?   	vl/cmd/maintenance-hold	[no test files]
?   	vl/cmd/nq_smoke	[no test files]
?   	vl/cmd/picture_htf_replay	[no test files]
?   	vl/cmd/planner_ab	[no test files]
?   	vl/cmd/planner_replay	[no test files]
?   	vl/cmd/research_export	[no test files]
?   	vl/cmd/sandbox-seed	[no test files]
ok  	vl/cmd/vl-activate	8.440s
ok  	vl/cmd/vl-updater	5.554s
?   	vl/cmd/vl-updater-bootstrap	[no test files]
?   	vl/cmd/w2w3_replay	[no test files]
ok  	vl/config	0.008s
?   	vl/crypto	[no test files]
ok  	vl/deploy	69.720s
ok  	vl/discipline	0.002s
?   	vl/docs/superpowers/reports/2026-09-04-research-conformance-data/d10dump	[no test files]
?   	vl/docs/superpowers/reports/2026-09-05-vet-08-stretch-data/complete-0905	[no test files]
ok  	vl/docs/superpowers/reports/2026-09-12-structural-stop/harness	0.007s
?   	vl/docs/superpowers/research/2026-09-12-backtest-structure-fade/harness	[no test files]
?   	vl/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness	[no test files]
ok  	vl/docs/superpowers/research/2026-09-16-round-23/harness	0.006s
ok  	vl/expectancy	0.050s
?   	vl/hook	[no test files]
ok  	vl/internal/activation	1.933s
ok  	vl/internal/censuswalk	1.922s
ok  	vl/internal/holdcli	0.091s
ok  	vl/internal/installpath	0.012s
ok  	vl/internal/retention	3.434s
ok  	vl/internal/retry	1.684s
?   	vl/internal/testhome	[no test files]
ok  	vl/internal/testtmpfs	0.005s
ok  	vl/internal/updateauth	28.696s
ok  	vl/internal/updaterbootstrap	2.386s
ok  	vl/internal/updaterjob	11.207s
ok  	vl/internal/updatersource	0.578s
ok  	vl/internal/updaterwire	0.836s
ok  	vl/internal/updaterwire/wireserver	0.744s
ok  	vl/internal/updaterworker	17.195s
?   	vl/internal/updaterworker/releasefixture	[no test files]
ok  	vl/internal/updaterworker/sqldriverpin	3.579s
?   	vl/internal/updatescheck	[no test files]
ok  	vl/kernel	7.786s
ok  	vl/kernel/mentor	46.299s
?   	vl/levelidentity	[no test files]
ok  	vl/logger	0.005s
?   	vl/manager	[no test files]
ok  	vl/market	0.281s
ok  	vl/mcp	25.024s
ok  	vl/mcp/provider	0.006s
?   	vl/provider/alpaca	[no test files]
ok  	vl/provider/databento	0.136s
ok  	vl/provider/ninjatrader	36.310s
?   	vl/provider/twelvedata	[no test files]
ok  	vl/researchsnapshot	5.818s
ok  	vl/safe	0.009s
?   	vl/security	[no test files]
ok  	vl/store	20.506s
ok  	vl/store/sqlitedriver	0.171s
ok  	vl/telegram	11.590s
ok  	vl/telegram/agent	1.838s
?   	vl/telegram/session	[no test files]
ok  	vl/telemetry	0.539s
ok  	vl/trader	151.590s
ok  	vl/trader/ninjatrader	14.297s
?   	vl/trader/types	[no test files]
?   	vl/web/node_modules/flatted/golang/pkg/flatted	[no test files]

```

## identity-probe

```text
=== RUN   TestAuditReferenceIdentityPanicProbe
    audit_identity_probe_test.go:21: REPRODUCED: valid reference identity causes a contained logging panic at production stampPlanIdentity
--- PASS: TestAuditReferenceIdentityPanicProbe (0.00s)
PASS
ok  	vl/trader	0.007s

```

## race

```text
ok  	vl/trader	1.141s
ok  	vl/kernel/mentor	1.039s

```

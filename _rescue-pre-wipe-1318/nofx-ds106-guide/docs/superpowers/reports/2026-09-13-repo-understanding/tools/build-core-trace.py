#!/usr/bin/env python3
"""Pin selected production declarations to one committed source snapshot.
This is a locator, not a call-graph or execution-proof generator.
"""
import hashlib,json,pathlib,re,subprocess,sys
source=pathlib.Path(sys.argv[1]); out=pathlib.Path(__file__).resolve().parent.parent
rev=subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()
items=[
('Transport','provider/ninjatrader/tcp_server.go','readLoop','Decodes broker frames; execution processing order must be verified separately.'),
('Ordered execution owner','provider/ninjatrader/ordered_execution.go','RegisterOrderedExecutionsFor','Exact account/symbol owner; preserves receive order before type fanout.'),
('Ordered entry delivery','provider/ninjatrader/ordered_execution.go','dispatchOrderedOrder','Applies received entry evidence before advisory consumers can reorder it.'),
('Ordered exit delivery','provider/ninjatrader/ordered_execution.go','dispatchOrderedClose','Applies received close evidence through the same serialized owner.'),
('Shared entry receipt','provider/ninjatrader/entry_receipt.go','NoteEntryExecution','Account/symbol cumulative evidence survives adapter replacement for shared server lifetime.'),
('Atomic position evidence','provider/ninjatrader/entry_receipt.go','PositionsForExecutionReceipt','Reads position snapshot and entry receipt under the same mutex; no guessed flat fallback.'),
('Owner installation','trader/ninjatrader/ordered_execution.go','InstallOrderedExecutions','Installs observation at successful construction; ordinary Stop retains it.'),
('Fill cache','trader/ninjatrader/tcp_trader.go','handleFill','Preserves actual exposure and refuses duplicate fully exited entry cache replay.'),
('Market data','market/data.go','GetWithTimeframes','Builds requested market context; futures provider path differs from legacy crypto.'),
('Canonical symbol','market/data.go','Normalize','Preserves the CME normalization boundary.'),
('Level evidence','kernel/levels_assemble.go','AssembleResearchLevels','Assembles raw, pool and seated candidates; heuristic scores are not probabilities.'),
('Weekly evidence','trader/auto_trader_weekly.go','weeklyDailyBars','Preserves daily input for CME-week aggregation.'),
('Weekly facts','kernel/weekly_bias.go','CompletedWeekCandles','Groups observations into completed Monday-governed weeks.'),
('Planner invocation','trader/auto_trader_planner.go','runPlannerReadCoreWithFacts','Machine facts and model response enter planner persistence/validation.'),
('Frozen setup identity','trader/structural_geometry.go','ResolveEntryGeometryZone','Rejects missing or ambiguous frozen source identity.'),
('First structural obstacle','trader/structural_geometry.go','FirstGeometryTarget','Chooses nearest complete sourced zone beyond the entry zone.'),
('Geometry','trader/structural_geometry.go','ComposeLevelFadeGeometry','Production wrapper freezes structural stop/target before admission.'),
('Geometry arithmetic','trader/structural_geometry.go','composeGeometry','Zone-edge buffer, outward rounding, costs and gross-R refusal; no ranking proof.'),
('Arm orchestration','trader/armed_executor.go','maybeManageArmedOrdersAt','Current-cycle authorization and gate results precede placement.'),
('Placement','trader/armed_executor.go','runArmedPlacementAt','Consumes currently eligible arm identities and broker/account evidence.'),
('One-contract guard','trader/one_contract.go','oneContractGuard','Account exposure and entry-order admission boundary.'),
('Session controls','trader/session_risk.go','sessionRiskGateAt','Session breaker/band verdict; does not alone establish daily-loss implementation.'),
('Daily reporting','trader/session_risk.go','bootRiskFacts','Reporting facts only; do not cite as executable daily-loss gate.'),
('Decision risk','kernel/engine_analysis.go','GetFullDecisionWithStrategy','Strategy decision/control pipeline; distinct from resting-arm placement.'),
('Position admission','trader/auto_trader_orders.go','ntHeldPosition','Broker position errors must remain unknown instead of flat.'),
('Decision long entry','trader/auto_trader_orders.go','executeOpenLongWithRecord','Actual decision entry call site; admission failure must prevent wire submission.'),
('Decision short entry','trader/auto_trader_orders.go','executeOpenShortWithRecord','Short counterpart requires the same ownership and exposure discipline.'),
('Resting limit','trader/ninjatrader/tcp_trader.go','PlaceLimitEntry','Registers identity before transmission; transmission is not broker acceptance.'),
('Stop entry','trader/ninjatrader/tcp_trader.go','PlaceStopEntry','Kind-specific stop entry adapter; distinct from protective stop placement.'),
('Cumulative entry','trader/armed_executor.go','onArmedOrderUpdate','Consumes actual entry state/quantity including positive terminal cancellations.'),
('Entry accounting','trader/armed_executor.go','materializeArmedEntry','Preserves cumulative entry quantity/notional and residual position accounting.'),
('Broker exit','trader/ninjatrader/close_sync.go','recordClose','Builds actual exit receipt with account and broker-order identity.'),
('Atomic exit','store/nt8_exit_receipt.go','ApplyNT8Exit','Receipt, actual fill and residual/P&L update share one transaction.'),
('Reconciliation','trader/ninjatrader/reconcile.go','reconcilePositions','Reconciles observations; a database row is not broker-flat proof.'),
('Positions truth','trader/ninjatrader/tcp_trader.go','GetPositions','Selected bound-account position snapshot and freshness admission.'),
]
rows=[]
for boundary,path,name,note in items:
 data=subprocess.check_output(['git','show',f'{rev}:{path}'],cwd=source)
 lines=data.decode().splitlines(); pat=re.compile(r'^func\s+(?:\([^)]*\)\s+)?'+re.escape(name)+r'\s*\(')
 matches=[i for i,line in enumerate(lines,1) if pat.search(line)]
 if len(matches)!=1:raise SystemExit(f'expected unique declaration {path}:{name}, found{matches}')
 line=matches[0];rows.append(dict(boundary=boundary,path=path,function=name,line=line,sha256=hashlib.sha256(data).hexdigest(),note=note))
(out/'core-trace.json').write_text(json.dumps(dict(revision=rev,scope='selected production declarations; not runtime call graph',entries=rows),indent=2)+'\n')
text=['# Core trading source trace','',f'Source snapshot: `{rev}`. Each link names an exact committed declaration. This is a selected source locator, not evidence that every branch ran. Detailed baseline function notes and connections are in the 30 review folders. Execution-order and integration tests must be read beside these source links.','', '| Boundary | Exact source | Responsibility / transfer limit |','| --- | --- | --- |']
for row in rows:
 url=f'https://github.com/johnwick2921-cyber/nofx/blob/{rev}/{row["path"]}#L{row["line"]}'
 text.append(f'| {row["boundary"]} | [{row["function"]}]({url}) | {row["note"]} |')
text.extend(['','Ordered execution and shared receipt lifetime are repaired at the source snapshot above; final verification is recorded in CHECKPOINT.md. Transport receipt, broker acceptance, execution, position reconciliation and permission to enter are separate facts. The root report records their verification status.',''])
(out/'CORE-TRACE.md').write_text('\n'.join(text))
print(f'{len(rows)} exact declarations pinned to {rev}')

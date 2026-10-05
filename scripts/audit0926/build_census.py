#!/usr/bin/env python3
"""Build the AUDIT-0926 settings census CSV (one row per setting).

Inputs (all under /tmp, produced by the same branch's scanners):
  audit0926-fields.json    strategy field spine (defined_at/type)
  audit0926-config.json    config.Config field spine (system_config)
  audit0926-env.json       env knob spine (defined_at/type/default/readers)
  audit0926-seeds.json     marshaller defaults + knob registry (all 200)
  audit0926-live-config.json  live strategy row a5b7662e config (4824B)
Reads /home/hoang/nofx/.env (secrets masked) and greps readers/UI/guide.

READ-ONLY. Writes docs/superpowers/reports/2026-09-26-audit-knobs-census.csv.
"""
import csv
import json
import os
import re
import subprocess

ROOT = "/home/hoang/nofx-ds-102-audit0926"
OUT = os.path.join(ROOT, "docs/superpowers/reports/2026-09-26-audit-knobs-census.csv")

def jload(p):
    raw = open(p).read()
    stripped = raw.lstrip()
    if stripped.startswith(("[", "{")):
        return json.loads(raw)
    return json.loads(raw[raw.index("{"):])

fields = jload("/tmp/audit0926-fields.json")
config_rows = jload("/tmp/audit0926-config.json")
env = jload("/tmp/audit0926-env.json")["knobs"]
seeds = jload("/tmp/audit0926-seeds.json")
defaults = seeds["defaults"]
reg_all = seeds["registry_all"]
live = json.load(open("/tmp/audit0926-live-config.json"))

# ---- flatten live config ----
def flatten(v, prefix, out):
    if isinstance(v, dict):
        for k, child in v.items():
            flatten(child, k if not prefix else prefix + "." + k, out)
    else:
        out[prefix] = v

liveflat = {}
flatten(live, "", liveflat)

# ---- .env (masked) ----
ENVF = "/home/hoang/nofx/.env"
envvals = {}
SECRET_HINT = re.compile(r"(KEY|SECRET|TOKEN|PASSWORD|PRIVATE|PASSPHRASE)", re.I)
for line in open(ENVF):
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    k, _, v = line.partition("=")
    k, v = k.strip(), v.strip().strip('"').strip("'")
    if SECRET_HINT.search(k) or len(v) > 24 or re.search(r"[+/=]{8}", v):
        v = "<set>"
    envvals.setdefault(k, v)  # first occurrence wins for display; dupes flagged below

def dup_env(name):
    n = 0
    for line in open(ENVF):
        if line.strip().startswith(name + "="):
            n += 1
    return n

# ---- reader grep: one pass for all Go field names ----
strat_leaves = [r for r in fields if not r["container"]]
leaf_names = {r["go_path"].split(".")[-1] for r in strat_leaves}
all_names = leaf_names | {r["go_path"].split(".")[-1] for r in config_rows}
pattern = re.compile(r"\b(" + "|".join(sorted(all_names)) + r")")

go_files = subprocess.run(
    ["git", "-C", ROOT, "ls-files", "*.go"], capture_output=True, text=True, check=True
).stdout.splitlines()
go_files = [f for f in go_files if not f.endswith("_test.go")]

reader_bucket = {n: [] for n in all_names}
for f in go_files:
    if f.startswith(("web/", "scripts/", "vendor/")):
        continue
    try:
        lines = open(os.path.join(ROOT, f), encoding="utf-8", errors="replace").read().splitlines()
    except OSError:
        continue
    for i, ln in enumerate(lines, 1):
        for m in pattern.finditer(ln):
            reader_bucket[m.group(1)].append("%s:%d" % (f, i))

def fmt_readers(name, defined_at):
    sites = []
    for s in reader_bucket.get(name, []):
        if s == defined_at or s.startswith("store/knob_registry"):
            continue
        if s not in sites:
            sites.append(s)
    if not sites:
        return "NONE"
    shown = sites[:8]
    extra = len(sites) - len(shown)
    return "; ".join(shown) + (("; +%d more" % extra) if extra > 0 else "")

# ---- UI / guide presence ----
have_rg = subprocess.run(["which", "rg"], capture_output=True).returncode == 0

def grep_presence(key, *dirs):
    found = []
    for d in dirs:
        dpath = os.path.join(ROOT, d)
        if not os.path.isdir(dpath):
            continue
        if have_rg:
            r = subprocess.run(["rg", "-l", "-F", key, dpath], capture_output=True, text=True)
            hits = [x for x in r.stdout.splitlines() if x]
        else:
            r = subprocess.run(["grep", "-rl", "-F", key, dpath], capture_output=True, text=True)
            hits = [x for x in r.stdout.splitlines() if x]
        if hits:
            found.append((d, hits[0].replace(ROOT + "/", "")))
    return found

# ---- hand maps ----
DAYPLAN_UI = {
    "picture_htf", "plan_mode", "min_scenario_quality", "replan_cap", "max_trades",
    "planner_timeframes", "proximity_filter_atr", "min_grade", "wake_on_level_events",
    "t1_currencies", "sessions_enabled", "one_setup_min_grade", "one_setup_enabled",
    "max_levels", "htf_seats", "approval_required", "flip_reread", "death_reread",
    "planner_model", "acceptance_rule",
}
NO_UI_GUIDE = {"write_time_feasibility", "geometry_reference_levels", "condition_status",
               "planner_fresh_tape"}
NO_UI_NO_GUIDE = {"planner_contract", "fade_or_wide_k", "last_entry_offset_min",
                  "eod_flat_offset_min"}
TYPE_ONLY = {"zone_max_pts", "min_hold_min", "entry_policy_default"}

def ui_fallback(path, folded):
    leaf = path.split(".")[-1]
    if path.startswith("ai_config.risk_control."):
        return "Studio RiskControlEditor control"
    if path.startswith("ai_config.indicators."):
        return "Studio IndicatorEditor control"
    if path.startswith("ai_config.coin_source."):
        return "Studio CoinSourceEditor control"
    if path.startswith("ai_config.custom_prompt."):
        return "Studio PromptSectionsEditor control"
    if path.startswith("ai_config.prompt_sections."):
        return "Studio PromptSectionsEditor control"
    if path.startswith("grid_config."):
        return "Studio GridConfigEditor control"
    if path.startswith("publish_config."):
        return "Studio PublishSettingsEditor control"
    if path.startswith("day_plan.sessions."):
        return "Studio session override block"
    if path.startswith("regime."):
        return "no control (nil-driven resolution)"
    if folded:
        return "no control (folded)"
    if leaf in DAYPLAN_UI or leaf in {"language", "strategy_type", "prompt_variant"}:
        return "Studio control (DayPlanEditor / strategy row selector)"
    if leaf in NO_UI_GUIDE:
        return "no control (guide documents it)"
    if leaf in NO_UI_NO_GUIDE:
        return "no control, no guide"
    if leaf in TYPE_ONLY:
        return "frontend type only, no control"
    return "no control"

# controls: one line each. Explicit first, then family text.
CTRL = {
    "strategy_type": "row selector: ai_trading vs grid_trading (custom MarshalJSON branch)",
    "language": "prompt language variant (en/zh); used at prompt build",
    "prompt_variant": "prompt variant selection; futures venue rule leaves it unset",
    "ai_config.coin_source.static_coins": "symbols traded by the strategy (live [MNQ])",
    "ai_config.indicators.klines.primary_timeframe": "primary decision timeframe",
    "ai_config.indicators.klines.timeframes": "indicator timeframes set (live [1h,4h,1d,15m,3m,5m])",
    "ai_config.indicators.enable_raw_klines": "raw price bars on/off",
    "ai_config.indicators.enable_ema": "EMA indicator block on/off (50/200 periods stored)",
    "ai_config.indicators.enable_rsi": "RSI block on/off (14)",
    "ai_config.indicators.enable_atr": "ATR block on/off (14) — feeds stop/anchor sizing",
    "ai_config.indicators.enable_volume": "volume profile block on/off",
    "ai_config.indicators.enable_funding_rate": "funding-rate block on/off",
    "ai_config.indicators.enable_svp": "session-volume-profile block on/off",
    "ai_config.indicators.enable_macd": "MACD block on/off (OFF live)",
    "ai_config.indicators.enable_boll": "Bollinger band block on/off (OFF live)",
    "ai_config.indicators.enable_oi": "open-interest block on/off (OFF live)",
    "ai_config.indicators.enable_quant_data": "quant analysis block on/off (OFF live)",
    "ai_config.indicators.enable_price_ranking": "rankings block on/off (OFF live)",
    "ai_config.custom_prompt.persona": "agent persona prompt text",
    "ai_config.custom_prompt.rules": "agent rule prompt text",
    "ai_config.custom_prompt.strategy_desc": "strategy description prompt text",
    "ai_config.custom_prompt.goal_description": "goal description prompt text",
    "ai_config.custom_prompt.persona_description": "persona description prompt text",
    "ai_config.prompt_sections.trading_frequency": "how often the bot should trade (live text claims exits SUSPENDED — stale, see P2-1)",
    "ai_config.prompt_sections.entry_standards": "entry standards prompt text",
    "ai_config.prompt_sections.confidence": "confidence criteria prompt text",
    "ai_config.prompt_sections.position_sizing": "position sizing prompt text",
    "ai_config.prompt_sections.risk_management": "risk management prompt text",
    "ai_config.risk_control.max_positions": "max open positions (live 3)",
    "ai_config.risk_control.leverage": "crypto leverage bound (live 5/5)",
    "ai_config.risk_control.min_risk_reward_ratio": "arm-gate R:R floor (armGateVerdictFor; live 2 vs code default 3.0)",
    "ai_config.risk_control.min_confidence": "minimum AI confidence to trade (live 60 vs default 75)",
    "ai_config.risk_control.max_margin_usage": "INEFFECTIVE — prompt text only, no gate reads it",
    "ai_config.risk_control.min_position_size": "INEFFECTIVE — crypto hardcode (12/60) in engine_position.go",
    "ai_config.risk_control.max_contracts_enabled": "INEFFECTIVE — no gate reads it",
    "ai_config.risk_control.notional_cap_enabled": "INEFFECTIVE — no gate reads it",
    "ai_config.risk_control.max_contracts_per_order": "futures contract cap per order (live 2)",
    "ai_config.risk_control.hold_discipline": "hold-discipline flag (live true)",
    "ai_config.risk_control.breakeven_enabled": "breakeven exit on/off (live false; trigger 40 stored)",
    "ai_config.risk_control.breakeven_trigger_points": "breakeven trigger points (stored 40; fires only when enabled)",
    "ai_config.risk_control.trailing_enabled": "trailing exit on/off (live false)",
    "ai_config.risk_control.guardrails_enabled": "guardrails master switch (live FALSE — stored caps bypassed, NOTE-3)",
    "ai_config.risk_control.daily_loss_limit": "daily loss cap — bypassed while master OFF (stored 450)",
    "ai_config.risk_control.daily_profit_target": "daily profit target — bypassed while master OFF (stored 900)",
    "ai_config.risk_control.max_trades_per_day": "max trades/day guardrail — bypassed while master OFF",
    "day_plan.plan_enabled": "day-plan master switch (live true)",
    "day_plan.plan_mode": "plan mode (live strict)",
    "day_plan.planner_model": "planner model binding",
    "day_plan.planner_timeframes": "planner timeframes (live [D,4h,1h,15m,5m])",
    "day_plan.planner_contract": "planner contract knob (nil=ON; A3); no Studio control — P2-4",
    "day_plan.planner_fresh_tape": "planner fresh-tape knob (nil=ON; A6)",
    "day_plan.one_setup_enabled": "one-setup mode (live FALSE [O])",
    "day_plan.one_setup_min_grade": "one-setup minimum scenario grade (default B)",
    "day_plan.picture_htf.enabled": "picture HTF mode (live true, SIM-only rule v1)",
    "day_plan.structure_map": "folded; stored true honoured (structure map section)",
    "day_plan.acceptance_rule": "folded; resolver maps every value onto 5m close",
    "day_plan.scenario_cap": "folded; stored 5 honoured",
    "day_plan.evening_digest": "folded; stored true honoured",
    "day_plan.realign_cap": "folded; stored 10 honoured",
    "day_plan.replan_cap": "replan cap per session (live 4 top + per-session 4)",
    "day_plan.sessions_enabled": "top-level session subset (live [NY]) — OVERRIDDEN per-session, CONFLICT (P2-6)",
    "day_plan.sessions.max_trades": "per-session entry cap via MaxTradesFor (live NY 10 · ASIA 7 · LONDON 10; 0 = no entries that session)",
    "day_plan.max_levels": "max plan levels (live 12)",
    "day_plan.proximity_filter_atr": "level proximity filter in ATR units (live 1)",
    "day_plan.htf_seats": "HTF seat count (default 2)",
    "day_plan.approval_required": "whether plan authoring needs approval (live false)",
    "day_plan.flip_reread": "flip re-read on/off (live true)",
    "day_plan.death_reread": "death re-read on/off (default ON)",
    "day_plan.wake_on_level_events": "master wake switch for level events",
    "day_plan.wake_on_htf_ob": "folded BUT keeps own effect: HTF OB wake class runs only when stored true (P2-5)",
    "day_plan.write_time_feasibility": "write-time feasibility gate (default ON)",
    "day_plan.geometry_reference_levels": "geometry reference levels gate (default ON)",
    "day_plan.zone_max_pts": "zone max points (default 10)",
    "day_plan.zone_rest_max_min": "zone rest cap minutes (default 30)",
    "day_plan.zone_place_within_pts": "zone placement band (default 25)",
    "day_plan.entry_policy_default": "default entry policy (market_in_zone)",
    "day_plan.min_hold_min": "minimum hold minutes (default 3)",
    "day_plan.min_scenario_quality": "minimum scenario quality grade",
    "day_plan.transition_standdown": "transition standdown (nil=ON)",
    "day_plan.eod_flat_offset_min": "EOD flat offset minutes (method-resolved)",
    "day_plan.last_entry_offset_min": "last-entry offset minutes (method-resolved)",
    "day_plan.condition_status": "condition status knob",
    "day_plan.fade_or_wide_k": "fade/wide multiplier k",
    "day_plan.t1_currencies": "T1 red-news hard-block currencies (default [USD])",
    "day_plan.htf_veto": "HTF veto mode (ON via Regime nil; env HTF_VETO_MODE=cross)",
    "day_plan.external_data_sources": "external data sources block — INEFFECTIVE (7 children, registry-pinned)",
    "regime.htf_veto": "regime-level HTF veto (nil-driven ON)",
    "regime.transition_standdown": "regime-level transition standdown",
    "regime.fixed_overhead": "candidate-unverified overhead field",
}

def ctrl_for(path, leaf, folded, is_container, kind):
    if kind == "env":
        return env_ctrl(path)
    if kind == "system_config":
        return sys_ctrl(path)
    if is_container:
        return "(container — see child rows)"
    if path in CTRL:
        return CTRL[path]
    if folded:
        return "folded knob — registry note: " + reg_note(path)[:80]
    note = reg_note(path)
    if note:
        return note[:110]
    return "strategy field (%s)" % path

def reg_entries(path):
    """Registry rows that match a census json path (exact or suffix)."""
    return [e for e in reg_all if e["path"] == path or path.endswith("." + e["path"])]

def reg_statuses(path):
    return [e["status"] for e in reg_entries(path)]

def reg_note(path):
    for e in reg_entries(path):
        if e.get("note"):
            return e["note"].replace("\n", " ")
    return ""

# Shipped defaults applied by RESOLVERS, not the struct (from the live boot
# lines + resolver code, report 2.3). The struct's zero value is NOT the
# effective default for these.
DEFAULTS_EXTRA = {
    "day_plan.htf_veto": "true (nil → ON)",
    "day_plan.transition_standdown": "true (nil → ON)",
    "day_plan.planner_contract": "true (nil → ON)",
    "day_plan.planner_fresh_tape": "true (nil → ON)",
    "day_plan.death_reread": "true (nil → ON)",
    "day_plan.write_time_feasibility": "true (nil → ON)",
    "day_plan.geometry_reference_levels": "true (nil → ON)",
    "day_plan.one_setup_min_grade": "\"B\"",
    "day_plan.htf_seats": "2",
    "day_plan.t1_currencies": "[\"USD\"]",
    "day_plan.entry_policy_default": "\"market_in_zone\"",
    "day_plan.zone_max_pts": "10",
    "day_plan.zone_rest_max_min": "30",
    "day_plan.zone_place_within_pts": "25",
    "day_plan.min_hold_min": "3",
    "day_plan.replan_cap": "4 (base)",
    "day_plan.acceptance_rule": "\"5m_close\" (folded: the one rule)",
    "day_plan.wake_min_interval_min": "30 (const, folded)",
}

# Env code defaults where the read site carries none (semantic default).
ENV_DEFAULT = {
    "STOP_ENTRY_SEAM": "off (semantic default; ruling says OFF)",
    "EXIT_MECHS_SUSPENDED": "0",
    "NOFX_TIMEZONE": "(unread; timezone pinned CT)",
    "FAST_MARKET_REASONING": "max (last .env occurrence wins; all 4 are max)",
}

def reg_consumers(path):
    cs = []
    for e in reg_entries(path):
        cs.extend(e.get("consumers") or [])
    return cs

ENV_CTRL = {
    "STOP_ENTRY_SEAM": "stop-entry placement seam — gate at armed_executor.go:1378; .env ON against the 2026-09-05 OFF ruling (P1-1)",
    "FAST_MARKET_REASONING": "fast-market AI reasoning effort — 4 duplicate .env keys, all max (loader last-wins)",
    "EXIT_MECHS_SUSPENDED": "exit-mechs suspension switch (0 = unsuspended); saved prompt text claims SUSPENDED (P2-1)",
    "NOFX_TIMEZONE": "no reader anywhere — timezone pinned to CT by owner rule (P2-2)",
    "CLAW402_DEFAULT_MODEL": "no reader — payment client uses const DefaultClaw402Model (P2-2)",
    "TRADING_MODE": "crypto vs futures path selection (live futures)",
    "NT_TRANSPORT": "NinjaTrader transport (live tcp)",
    "ARMED_TEST_SEAM": "armed-orders test seam (live off)",
    "HISTORICAL_IMPORT_SEAM": "historical import seam (live on)",
    "HTF_VETO_MODE": "HTF veto mode (live cross)",
    "EOD_FLAT_LIMIT_TICKS": "EOD limit-close ticks (live 2)",
    "EOD_FLAT_MARKET_AFTER_SEC": "EOD market-close after seconds (live 10)",
    "RISK_MAX_DAILY_LOSS_USD": "server-side daily loss backstop (default 500)",
    "RISK_MAX_CONCURRENT_TRADES": "server-side concurrent trades backstop (default 2)",
    "RISK_MAX_NOTIONAL_USD": "server-side notional backstop (default 50k; loaded but enforced nowhere per config.go 6.8 note)",
    "AI_MAX_TOKENS": "executor AI max tokens (live <set>)",
    "AI_HTTP_TIMEOUT_SECONDS": "executor AI HTTP timeout (live 600)",
    "AI_MAX_RETRIES": "executor AI retries (live 2)",
    "AI_EXEC_REASONING": "executor reasoning effort (default fast)",
    "AI_PLAN_REASONING": "planner reasoning effort (default max)",
    "DATABENTO_API_KEY": "Databento key — historical/backfill only; NT8 is the live source",
    "NINJATRADER_DATA_DIR": "legacy CSV bridge path; live TCP path does not use it",
    "NT_EXTRA_SYMBOLS": "extra NT symbols (live ES)",
    "NT_RUNTIME_SYMBOLS": "runtime symbol discovery (live true)",
}

def env_ctrl(name):
    if name in ENV_CTRL:
        return ENV_CTRL[name]
    if name.startswith("AI_"):
        return "AI client parameter (timeout/tokens/reasoning/stream)"
    if name.startswith("ARM") or name.startswith("ARM_"):
        return "armed-orders placement/retirement knob"
    if name.startswith("BD_"):
        return "breakdown entry-law knob"
    if name.startswith("STRUCTURE_"):
        return "structure-map knob"
    if name.startswith("TOUCH_"):
        return "touch-telemetry knob"
    if name.startswith("STALE_"):
        return "stale-bar/working reaper knob"
    if name.startswith("TRANSITION_"):
        return "transition standdown knob"
    if name.startswith("NT_"):
        return "NinjaTrader bridge knob"
    if name.startswith("CLAW402_"):
        return "Claw402 payment-provider knob"
    if name.startswith("RISK_"):
        return "server-side risk limit"
    if "_SEAM" in name:
        return "test seam"
    if "_TEST" in name or name.startswith("BINANCE_") or name.startswith("KUCOIN_") or name.startswith("INDODAX_") or name.startswith("ALPACA_") or name.startswith("ANTHROPIC_"):
        return "test/demo credentials"
    if "GOLDEN" in name or "REHEARSAL" in name or "PARITY" in name or "PROOF" in name or name.startswith("NOFX_DEMO") or name.startswith("CENSUS_") or name.startswith("UPDATE_") or name.startswith("STAGE_A_"):
        return "test-harness golden/replay knob"
    if name.startswith("SANDBOX_"):
        return "sandbox knob"
    if name.startswith("BARS_KEY_") or name.startswith("BAR_"):
        return "bar cache/persistence knob"
    if name.startswith("WEEKLY_"):
        return "weekly level counter knob"
    if name.startswith("CONFIRM_"):
        return "cancel-confirmation knob"
    return "env knob"

def sys_ctrl(name):
    return "config.Config field loaded from env at boot (see env rows for the live value)"

# ---- status ----
def status_for(row, kind, path, readers_text, name):
    if kind == "strategy":
        if row["container"]:
            return "LIVE"
        statuses = reg_statuses(path)
        if not statuses:
            return "UNREGISTERED"
        if "folded" in statuses:
            return "SHADOWED"
        if "ineffective" in statuses:
            return "DEAD"
        if "candidate-unverified" in statuses and "live" not in statuses:
            return "LIVE" if readers_text != "NONE" else "DEAD"
        if "live" in statuses:
            return "LIVE"
        return "UNREGISTERED"
    if kind == "system_config":
        return "LIVE" if readers_text != "NONE" else "DEAD"
    # env
    if name == "STOP_ENTRY_SEAM" or name == "FAST_MARKET_REASONING":
        return "CONFLICT"
    if readers_text == "NONE":
        return "DEAD"
    return "LIVE"

# ---- label_guide_match ----
def label_guide(path, kind, name, folded):
    if kind == "strategy" and folded:
        return "n/a (folded knob)"
    if kind == "strategy":
        leaf = path.split(".")[-1]
        ui = grep_presence(leaf, "web/src/components/strategy")
        guide = grep_presence(leaf, "web/src/guide/content")
        if ui and guide:
            return "yes"
        if guide:
            return "no (guide only, no Studio control)"
        if ui:
            return "no (Studio only, no guide)"
        return "n/a (no Studio control, no guide)"
    if kind == "env":
        guide = grep_presence(name, "web/src/guide/content")
        return "yes" if guide else "n/a (env knob, no Studio control)"
    return "n/a (system_config)"

# ---- assemble rows ----
csv_rows = []
def add(name, kind, defined_at, typ, code_default, ui_fb, live_val, origin, readers, controls, lgm, status):
    csv_rows.append({
        "name": name, "kind": kind, "defined_at": defined_at, "type": typ,
        "code_default": code_default, "ui_fallback": ui_fb, "live_value": live_val,
        "origin": origin, "readers": readers, "controls": controls,
        "label_guide_match": lgm, "status": status,
    })

def zero_default(typ):
    t = typ.split(".")[-1]
    if t == "bool":
        return "false"
    if t in ("int", "int64", "float64", "float32", "int32"):
        return "0"
    if typ.startswith("[]") or typ.startswith("map["):
        return "[]" if typ.startswith("[]") else "{}"
    if t == "string":
        return '""'
    return "(zero)"

def livefmt(v):
    s = json.dumps(v, ensure_ascii=False)
    return s[:44] + ("…" if len(s) > 44 else "")

for r in fields:
    path = r["json_path"]
    folded = any(e["status"] == "folded" for e in reg_entries(path))
    if r["container"]:
        add(path, "strategy", r["defined_at"], r["type"], "(object)",
            "(children)", "(object)", "—", "—", "(container — see child rows)",
            "n/a (container)", "LIVE")
        continue
    leaf = path.split(".")[-1]
    gofield = r["go_path"].split(".")[-1]
    readers = fmt_readers(gofield, r["defined_at"])
    regc = [c for c in reg_consumers(path) if c not in readers]
    if regc:
        if readers == "NONE":
            readers = "; ".join(regc[:8])
        else:
            readers += "; " + "; ".join(regc[: max(0, 8 - readers.count(";") - 1)])
    d = defaults.get(path)
    if d is not None:
        code_default = json.dumps(d, ensure_ascii=False)
    elif path in DEFAULTS_EXTRA:
        code_default = DEFAULTS_EXTRA[path]
    else:
        code_default = zero_default(r["type"])
    if path in liveflat:
        lv, origin = livefmt(liveflat[path]), "O"
    else:
        lv, origin = "(absent → code default)", "default"
    if path == "day_plan.sessions_enabled":
        status = "CONFLICT"
    else:
        status = status_for(r, "strategy", path, readers, leaf)
    add(path, "strategy", r["defined_at"], r["type"], code_default,
        ui_fallback(path, folded), lv, origin, readers,
        ctrl_for(path, leaf, folded, False, "strategy"),
        label_guide(path, "strategy", leaf, folded), status)

for e in env:
    name = e["name"]
    readers = "; ".join(e["readers"][:8])
    readers = readers if readers else "NONE"
    if e["readers"] and not any(not x.startswith(("scripts/", "cmd/")) or x.endswith("_test.go") for x in []):
        pass
    non_test = [x for x in e["readers"] if not x.endswith("_test.go") and not x.startswith("scripts/")]
    readers_disp = readers
    lv = envvals.get(name, "(not set → code default)")
    origin = "E" if name in envvals else "default"
    if dup_env(name) > 1:
        lv += " (%d dup keys in .env)" % dup_env(name)
    if name == "FAST_MARKET_REASONING":
        status = "CONFLICT"
    elif not non_test:
        status = "DEAD"
    else:
        status = status_for(None, "env", name, readers_disp, name)
    add(name, "env", e["defined_at"], e["type"],
        e["code_default"] or ENV_DEFAULT.get(name, "\"\""),
        "n/a (env)", lv, origin, readers_disp,
        env_ctrl(name), label_guide(name, "env", name, False), status)

for r in config_rows:
    name = r["json_path"]
    leaf = name.split(".")[-1]
    readers = fmt_readers(leaf, r["defined_at"])
    add(name, "system_config", r["defined_at"], r["type"], zero_default(r["type"]),
        "n/a (config)", "(env rows carry live values)", "default", readers,
        sys_ctrl(name), "n/a (system_config)", status_for(r, "system_config", name, readers, name))

# .env keys with no code reader at all join the census as DEAD env rows (P2-2),
# EXCEPT keys read indirectly (via a constant or an envconfig tag) — those are
# LIVE with the indirect readers.
def plain_readers(name):
    r = subprocess.run(
        ["grep", "-rn", "-F", '"%s"' % name, "--include=*.go", ROOT],
        capture_output=True, text=True)
    sites = []
    for ln in r.stdout.splitlines():
        if "_test.go" in ln or "/web/" in ln or "/scripts/" in ln or "/vendor/" in ln:
            continue
        p = ln.split(":", 2)
        if len(p) == 3:
            s = "%s:%s" % (p[0].replace(ROOT + "/", ""), p[1])
            if s not in sites:
                sites.append(s)
    return sites

for key in envvals:
    if not any(r["name"] == key for r in csv_rows):
        if key == "CLAW402_DEFAULT_MODEL":
            # writer-only: onboarding os.Setenv; the payment client uses the
            # hardcoded const DefaultClaw402Model — the .env value is unread
            add(key, "env", "api/handler_onboarding.go:72 (Setenv — writer only)",
                "string", '""', "n/a (env)", envvals[key] + " (unread)", "E", "NONE",
                "writer-only: the payment client uses const DefaultClaw402Model (P2-2)",
                "n/a (no reader)", "DEAD")
            continue
        pr = plain_readers(key)
        if pr:
            add(key, "env", pr[0], "string", '""', "n/a (env)", envvals[key],
                "E", "; ".join(pr[:8]), "read indirectly (const/envconfig) — see readers",
                "n/a (env knob)", "LIVE")
        else:
            add(key, "env", ".env only (no reader anywhere)",
                "string", '""', "n/a (env)", envvals[key] + " (unread)", "E", "NONE",
                "no reader anywhere — the value changes nothing (P2-2 class)", "n/a (no reader)", "DEAD")

with open(OUT, "w", newline="") as f:
    w = csv.DictWriter(f, fieldnames=[
        "name", "kind", "defined_at", "type", "code_default", "ui_fallback",
        "live_value", "origin", "readers", "controls", "label_guide_match", "status"])
    w.writeheader()
    for r in csv_rows:
        w.writerow(r)

from collections import Counter
c = Counter(r["status"] for r in csv_rows)
print("rows:", len(csv_rows))
print("by kind:", Counter(r["kind"] for r in csv_rows))
print("by status:", dict(c))
print("written:", OUT)

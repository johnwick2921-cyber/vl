// One-time browser-storage migration onto the VL names, imported FIRST in
// main.tsx as a side-effect import: ESM evaluates imports in order and no
// module-scope localStorage reader exists, so every later reader sees the
// migrated state. The pre-rename literals below stay until R5.
//
// Rules:
//  - copy when the new key is absent (safe with two tabs open);
//  - both present: the chat keys merge (legacy messages whose id is not
//    already present are appended), the other keys let the VL value win;
//  - an old key is deleted only after the new value is written and read back;
//  - QuotaExceededError retries ONCE as an in-task move (hold the value,
//    remove the old key, set the VL key; on failure restore the old key in
//    the same task);
//  - a parse failure keeps the old key and retries on the next load;
//  - a failure on one key never aborts the others.

export const VL_USER_MODE_KEY = 'vl.userMode'
export const VL_BEGINNER_ONBOARDING_COMPLETED_KEY =
  'vl.beginnerOnboardingCompleted'
export const VL_AGENT_CHAT_KEY = 'vl.agentChat'
export const VL_AGENT_CHAT_PREFIX = 'vl.agentChat:'
export const VL_AGENT_CHAT_DRAFT_PREFIX = 'vl.agentChatDraft:'
export const VL_LEGACY_KEYS_LEFT_KEY = 'vl.legacyKeysLeft'
export const VL_MIGRATED_AT_KEY = 'vl.migratedAt'

export function vlAgentChatKey(userId?: string): string {
  return `${VL_AGENT_CHAT_PREFIX}${userId || 'guest'}`
}

export function vlAgentChatDraftKey(userId?: string): string {
  return `${VL_AGENT_CHAT_DRAFT_PREFIX}${userId || 'guest'}`
}

// The pre-rename keys. The matching order matters for the chat family:
// the draft prefix first, then the chat prefix, then the bare key by exact
// equality (the bare key is a prefix of the other two).
const LEGACY_SCALAR_KEYS: ReadonlyArray<readonly [string, string]> = [
  ['nofx_user_mode', VL_USER_MODE_KEY],
  ['nofx_beginner_onboarding_completed', VL_BEGINNER_ONBOARDING_COMPLETED_KEY],
]
const LEGACY_CHAT_BASE = 'nofxi-agent-chat'
const LEGACY_CHAT_PREFIX = 'nofxi-agent-chat:'
const LEGACY_CHAT_DRAFT_PREFIX = 'nofxi-agent-chat-draft:'

type LegacyKind = 'scalar' | 'chat' | 'draft'

interface LegacyItem {
  legacy: string
  target: string
  kind: LegacyKind
}

function collectLegacy(storage: Storage): LegacyItem[] {
  const items: LegacyItem[] = []
  const seen = new Set<string>()
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i)
    if (!key || seen.has(key)) continue
    let target: string | undefined
    let kind: LegacyKind | undefined
    if (key.startsWith(LEGACY_CHAT_DRAFT_PREFIX)) {
      target =
        VL_AGENT_CHAT_DRAFT_PREFIX + key.slice(LEGACY_CHAT_DRAFT_PREFIX.length)
      kind = 'draft'
    } else if (key.startsWith(LEGACY_CHAT_PREFIX)) {
      target = VL_AGENT_CHAT_PREFIX + key.slice(LEGACY_CHAT_PREFIX.length)
      kind = 'chat'
    } else {
      const scalar = LEGACY_SCALAR_KEYS.find(([legacy]) => legacy === key)
      if (scalar) {
        target = scalar[1]
        kind = 'scalar'
      }
    }
    if (target && kind) {
      seen.add(key)
      items.push({ legacy: key, target, kind })
    }
  }
  if (
    storage.getItem(LEGACY_CHAT_BASE) !== null &&
    !seen.has(LEGACY_CHAT_BASE)
  ) {
    items.push({
      legacy: LEGACY_CHAT_BASE,
      target: VL_AGENT_CHAT_KEY,
      kind: 'chat',
    })
  }
  return items
}

function isQuotaError(err: unknown): boolean {
  if (typeof err !== 'object' || err === null) return false
  const name = (err as { name?: unknown }).name
  if (name === 'QuotaExceededError') return true
  return (
    typeof DOMException !== 'undefined' &&
    err instanceof DOMException &&
    err.name === 'QuotaExceededError'
  )
}

// Hold the value, remove the old key, set the VL key; on failure restore the
// old key in the same task. Used once per key, only after a quota error.
function retryMoveOnce(
  storage: Storage,
  legacy: string,
  target: string,
  value: string
): void {
  try {
    storage.removeItem(legacy)
    storage.setItem(target, value)
  } catch {
    try {
      storage.setItem(legacy, value)
    } catch {
      // best effort; the pass never aborts
    }
  }
}

// Write the new value, read it back, and only then delete the old key.
function writeAndVerify(
  storage: Storage,
  legacy: string,
  target: string,
  value: string
): void {
  try {
    storage.setItem(target, value)
    if (storage.getItem(target) !== value) return
    storage.removeItem(legacy)
  } catch (err) {
    if (isQuotaError(err)) retryMoveOnce(storage, legacy, target, value)
    // any other failure keeps the old key; the pass never aborts
  }
}

function parseMessages(raw: string): Array<{ id?: unknown }> {
  const parsed: unknown = JSON.parse(raw)
  return Array.isArray(parsed) ? (parsed as Array<{ id?: unknown }>) : []
}

// Returns the merged JSON, or null when either side fails to parse (the old
// key is then kept and the next load retries).
function mergeChat(existingRaw: string, legacyRaw: string): string | null {
  try {
    const existing = parseMessages(existingRaw)
    const legacy = parseMessages(legacyRaw)
    const ids = new Set<unknown>()
    for (const message of existing) {
      if (message?.id !== undefined) ids.add(message.id)
    }
    const extra = legacy.filter(
      (message) => message?.id === undefined || !ids.has(message.id)
    )
    return JSON.stringify([...existing, ...extra])
  } catch {
    return null
  }
}

function migrateOne(storage: Storage, item: LegacyItem): void {
  const legacyValue = storage.getItem(item.legacy)
  if (legacyValue === null) return
  const existing = storage.getItem(item.target)
  if (existing === null) {
    writeAndVerify(storage, item.legacy, item.target, legacyValue)
    return
  }
  if (item.kind === 'chat') {
    const merged = mergeChat(existing, legacyValue)
    if (merged === null) return
    writeAndVerify(storage, item.legacy, item.target, merged)
    return
  }
  // Both present for the scalar keys: the VL value wins.
  storage.removeItem(item.legacy)
}

export function countLegacyKeys(storage: Storage): number {
  let count = 0
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i)
    if (!key) continue
    if (
      key.startsWith(LEGACY_CHAT_DRAFT_PREFIX) ||
      key.startsWith(LEGACY_CHAT_PREFIX) ||
      key === LEGACY_CHAT_BASE ||
      LEGACY_SCALAR_KEYS.some(([legacy]) => legacy === key)
    ) {
      count++
    }
  }
  return count
}

export function runStorageMigration(storage: Storage): {
  legacyKeysLeft: number
} {
  for (const item of collectLegacy(storage)) {
    migrateOne(storage, item)
  }
  const left = countLegacyKeys(storage)
  storage.setItem(VL_LEGACY_KEYS_LEFT_KEY, String(left))
  if (left === 0 && storage.getItem(VL_MIGRATED_AT_KEY) === null) {
    storage.setItem(VL_MIGRATED_AT_KEY, new Date().toISOString())
  }
  return { legacyKeysLeft: left }
}

export function storageMigrationLine(storage: Storage, origin: string): string {
  const left = countLegacyKeys(storage)
  const migratedAt = storage.getItem(VL_MIGRATED_AT_KEY)
  if (left === 0 && migratedAt) {
    return `This browser (${origin}): storage on VL names since ${migratedAt}`
  }
  return `This browser (${origin}): ${left} old storage keys NOT migrated — reload; if it stays, tell the CTO`
}

// Side effect: migrate once per page load, before any accessor runs.
if (
  typeof window !== 'undefined' &&
  typeof window.localStorage !== 'undefined'
) {
  try {
    runStorageMigration(window.localStorage)
  } catch {
    // storage is unavailable (privacy mode etc.); the app runs without it
  }
}

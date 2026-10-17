import { describe, expect, it } from 'vitest'
import {
  VL_AGENT_CHAT_KEY,
  VL_AGENT_CHAT_DRAFT_PREFIX,
  VL_AGENT_CHAT_PREFIX,
  VL_BEGINNER_ONBOARDING_COMPLETED_KEY,
  VL_LEGACY_KEYS_LEFT_KEY,
  VL_MIGRATED_AT_KEY,
  VL_USER_MODE_KEY,
  countLegacyKeys,
  runStorageMigration,
  storageMigrationLine,
  vlAgentChatDraftKey,
  vlAgentChatKey,
} from './storageMigration'

function createStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear() {
      data.clear()
    },
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    key(index: number) {
      return Array.from(data.keys())[index] ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, value)
    },
  }
}

// A storage whose setItem throws QuotaExceededError exactly ONCE for the
// first write to the given key, then behaves normally.
function createQuotaStorage(failFor: string): Storage {
  const inner = createStorage()
  let failed = false
  return {
    get length() {
      return inner.length
    },
    clear() {
      inner.clear()
    },
    getItem(key: string) {
      return inner.getItem(key)
    },
    key(index: number) {
      return inner.key(index)
    },
    removeItem(key: string) {
      inner.removeItem(key)
    },
    setItem(key: string, value: string) {
      if (!failed && key === failFor) {
        failed = true
        const err = new Error('quota')
        ;(err as Error & { name: string }).name = 'QuotaExceededError'
        throw err
      }
      inner.setItem(key, value)
    },
  }
}

describe('storageMigration', () => {
  it('copies a scalar key when the VL key is absent and deletes the old key', () => {
    const storage = createStorage()
    storage.setItem('nofx_user_mode', 'advanced')

    const result = runStorageMigration(storage)

    expect(storage.getItem(VL_USER_MODE_KEY)).toBe('advanced')
    expect(storage.getItem('nofx_user_mode')).toBeNull()
    expect(result.legacyKeysLeft).toBe(0)
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).not.toBeNull()
  })

  it('lets the VL value win when both scalar keys are present', () => {
    const storage = createStorage()
    storage.setItem('nofx_user_mode', 'advanced')
    storage.setItem(VL_USER_MODE_KEY, 'beginner')

    runStorageMigration(storage)

    expect(storage.getItem(VL_USER_MODE_KEY)).toBe('beginner')
    expect(storage.getItem('nofx_user_mode')).toBeNull()
  })

  it('migrates every scalar key', () => {
    const storage = createStorage()
    storage.setItem('nofx_user_mode', 'advanced')
    storage.setItem('nofx_beginner_onboarding_completed', 'true')

    runStorageMigration(storage)

    expect(storage.getItem(VL_USER_MODE_KEY)).toBe('advanced')
    expect(storage.getItem(VL_BEGINNER_ONBOARDING_COMPLETED_KEY)).toBe('true')
    expect(countLegacyKeys(storage)).toBe(0)
  })

  it('migrates the per-user chat and draft keys', () => {
    const storage = createStorage()
    storage.setItem('nofxi-agent-chat:user-1', JSON.stringify([{ id: '1' }]))
    storage.setItem('nofxi-agent-chat-draft:user-1', 'draft text')

    runStorageMigration(storage)

    expect(storage.getItem(vlAgentChatKey('user-1'))).toBe(
      JSON.stringify([{ id: '1' }])
    )
    expect(storage.getItem('nofxi-agent-chat:user-1')).toBeNull()
    expect(storage.getItem(vlAgentChatDraftKey('user-1'))).toBe('draft text')
    expect(storage.getItem('nofxi-agent-chat-draft:user-1')).toBeNull()
  })

  it('migrates the bare chat key by exact equality only', () => {
    const storage = createStorage()
    storage.setItem('nofxi-agent-chat', JSON.stringify([{ id: '1' }]))

    runStorageMigration(storage)

    expect(storage.getItem(VL_AGENT_CHAT_KEY)).toBe(
      JSON.stringify([{ id: '1' }])
    )
    expect(storage.getItem('nofxi-agent-chat')).toBeNull()
  })

  it('merges chat keys by id when both are present, then deletes the old key', () => {
    const storage = createStorage()
    storage.setItem(
      vlAgentChatKey('user-1'),
      JSON.stringify([{ id: '1', text: 'kept' }])
    )
    storage.setItem(
      'nofxi-agent-chat:user-1',
      JSON.stringify([
        { id: '1', text: 'duplicate' },
        { id: '2', text: 'legacy-only' },
      ])
    )

    runStorageMigration(storage)

    const merged = JSON.parse(storage.getItem(vlAgentChatKey('user-1')) ?? '[]')
    expect(merged).toEqual([
      { id: '1', text: 'kept' },
      { id: '2', text: 'legacy-only' },
    ])
    expect(storage.getItem('nofxi-agent-chat:user-1')).toBeNull()
  })

  it('keeps the old key when the merge cannot parse', () => {
    const storage = createStorage()
    storage.setItem(vlAgentChatKey('user-1'), 'not-json')
    storage.setItem('nofxi-agent-chat:user-1', JSON.stringify([{ id: '2' }]))

    const result = runStorageMigration(storage)

    expect(storage.getItem('nofxi-agent-chat:user-1')).toBe(
      JSON.stringify([{ id: '2' }])
    )
    expect(result.legacyKeysLeft).toBe(1)
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).toBeNull()
  })

  it('retries a quota error once as an in-task move', () => {
    const storage = createQuotaStorage(VL_USER_MODE_KEY)
    storage.setItem('nofx_user_mode', 'advanced')

    runStorageMigration(storage)

    expect(storage.getItem(VL_USER_MODE_KEY)).toBe('advanced')
    expect(storage.getItem('nofx_user_mode')).toBeNull()
  })

  it('restores the old key when the quota retry also fails', () => {
    const inner = createStorage()
    inner.setItem('nofx_user_mode', 'advanced')
    let remaining = 2
    const storage = {
      get length() {
        return inner.length
      },
      clear() {
        inner.clear()
      },
      getItem(key: string) {
        return inner.getItem(key)
      },
      key(index: number) {
        return inner.key(index)
      },
      removeItem(key: string) {
        inner.removeItem(key)
      },
      setItem(key: string, value: string) {
        if (remaining > 0) {
          remaining--
          const err = new Error('quota')
          ;(err as Error & { name: string }).name = 'QuotaExceededError'
          throw err
        }
        inner.setItem(key, value)
      },
    }

    runStorageMigration(storage)

    // The retry itself failed: the old key is restored, nothing is lost.
    expect(storage.getItem('nofx_user_mode')).toBe('advanced')
    expect(storage.getItem(VL_USER_MODE_KEY)).toBeNull()
  })

  it('writes the marker counts and migratedAt only when nothing is left', () => {
    const storage = createStorage()
    storage.setItem('nofx_user_mode', 'advanced')
    storage.setItem('nofxi-agent-chat', 'not-json-chat-both-sides-absent')

    runStorageMigration(storage)

    expect(storage.getItem(VL_LEGACY_KEYS_LEFT_KEY)).toBe('0')
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).not.toBeNull()

    storage.setItem('nofx_beginner_onboarding_completed', 'true')
    runStorageMigration(storage)
    // migratedAt is written once, never overwritten — but with a legacy key
    // present it is not written again; the earlier value stays.
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).not.toBeNull()
  })

  it('does not write migratedAt while legacy keys remain', () => {
    const storage = createStorage()
    // A merge that fails to parse keeps the old key, so a legacy key remains.
    storage.setItem(VL_AGENT_CHAT_KEY, '{broken')
    storage.setItem('nofxi-agent-chat', '{broken')

    runStorageMigration(storage)

    expect(storage.getItem('nofxi-agent-chat')).toBe('{broken')
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).toBeNull()
  })

  it('builds the marker line for both states', () => {
    const storage = createStorage()
    storage.setItem(VL_MIGRATED_AT_KEY, '2026-09-30T12:00:00.000Z')
    expect(storageMigrationLine(storage, 'http://localhost:8080')).toBe(
      'This browser (http://localhost:8080): storage on VL names since 2026-09-30T12:00:00.000Z'
    )

    const dirty = createStorage()
    dirty.setItem('nofx_user_mode', 'advanced')
    expect(storageMigrationLine(dirty, 'http://localhost:3000')).toBe(
      'This browser (http://localhost:3000): 1 old storage keys NOT migrated — reload; if it stays, tell the CTO'
    )
  })
})

// ── W1 survivor folds (CHECK D2-WEB): writeAndVerify's failure paths ─────────
// A migration that deletes the old key without a trusted new value loses data.
// Each case proves the old key survives with its original value and the marker
// counts the key as NOT migrated.

// A storage whose setItem succeeds but whose getItem returns a DIFFERENT value
// for the new key: the read-back disagrees, so the new key must not be trusted.
function createDisagreeingReadbackStorage(targetKey: string): Storage {
  const inner = createStorage()
  return {
    get length() {
      return inner.length
    },
    clear() {
      inner.clear()
    },
    getItem(key: string) {
      if (key === targetKey) {
        return inner.getItem(key) === null ? null : 'corrupted-readback'
      }
      return inner.getItem(key)
    },
    key(index: number) {
      return inner.key(index)
    },
    removeItem(key: string) {
      inner.removeItem(key)
    },
    setItem(key: string, value: string) {
      inner.setItem(key, value)
    },
  }
}

describe('writeAndVerify failure paths (W1)', () => {
  it('keeps the old key when the read-back disagrees', () => {
    const storage = createDisagreeingReadbackStorage(VL_USER_MODE_KEY)
    storage.setItem('nofx_user_mode', 'advanced')

    const result = runStorageMigration(storage)

    expect(storage.getItem('nofx_user_mode')).toBe('advanced')
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).toBeNull()
    expect(result.legacyKeysLeft).toBe(1)
    expect(storage.getItem(VL_LEGACY_KEYS_LEFT_KEY)).toBe('1')
  })

  it('counts the key as NOT migrated when a quota error kills the retry', () => {
    const inner = createStorage()
    inner.setItem('nofx_user_mode', 'advanced')
    let remaining = 2
    const storage = {
      get length() {
        return inner.length
      },
      clear() {
        inner.clear()
      },
      getItem(key: string) {
        return inner.getItem(key)
      },
      key(index: number) {
        return inner.key(index)
      },
      removeItem(key: string) {
        inner.removeItem(key)
      },
      setItem(key: string, value: string) {
        if (remaining > 0) {
          remaining--
          const err = new Error('quota')
          ;(err as Error & { name: string }).name = 'QuotaExceededError'
          throw err
        }
        inner.setItem(key, value)
      },
    }

    const result = runStorageMigration(storage)

    expect(storage.getItem('nofx_user_mode')).toBe('advanced')
    expect(storage.getItem(VL_USER_MODE_KEY)).toBeNull()
    expect(result.legacyKeysLeft).toBe(1)
    expect(storage.getItem(VL_LEGACY_KEYS_LEFT_KEY)).toBe('1')
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).toBeNull()
  })

  it('keeps the old key when the old chat value does not parse', () => {
    const storage = createStorage()
    storage.setItem(VL_AGENT_CHAT_KEY, '{broken')
    storage.setItem('nofxi-agent-chat', '{broken')

    const result = runStorageMigration(storage)

    expect(storage.getItem('nofxi-agent-chat')).toBe('{broken')
    expect(result.legacyKeysLeft).toBe(1)
    expect(storage.getItem(VL_LEGACY_KEYS_LEFT_KEY)).toBe('1')
    expect(storage.getItem(VL_MIGRATED_AT_KEY)).toBeNull()
  })
})

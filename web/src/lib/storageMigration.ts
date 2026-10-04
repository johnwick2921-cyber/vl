// R5 rename (2026-10-02): the pre-rename storage migration is REMOVED — the
// compat layer is gone. Browsers keep whatever they stored under the VL keys;
// pre-rename keys are no longer read or migrated. This module holds only the
// VL storage-key constants the readers share.
export const VL_USER_MODE_KEY = 'vl.userMode'
export const VL_BEGINNER_ONBOARDING_COMPLETED_KEY =
  'vl.beginnerOnboardingCompleted'
export const VL_AGENT_CHAT_KEY = 'vl.agentChat'
export const VL_AGENT_CHAT_PREFIX = 'vl.agentChat:'
export const VL_AGENT_CHAT_DRAFT_PREFIX = 'vl.agentChatDraft:'
export const VL_MIGRATED_AT_KEY = 'vl.migratedAt'

export function vlAgentChatKey(userId?: string): string {
  return `${VL_AGENT_CHAT_PREFIX}${userId || 'guest'}`
}

export function vlAgentChatDraftKey(userId?: string): string {
  return `${VL_AGENT_CHAT_DRAFT_PREFIX}${userId || 'guest'}`
}

// The Settings/Updates pages show one line about browser storage. R5: there is
// no migration any more, so the line reads the migration stamp when it exists
// and the plain fact otherwise — never a fabricated status.
export function storageMigrationLine(storage: Storage, origin: string): string {
  const migratedAt = storage.getItem(VL_MIGRATED_AT_KEY)
  if (migratedAt) {
    return `This browser (${origin}): storage on VL names since ${migratedAt}`
  }
  return `This browser (${origin}): storage on VL names`
}

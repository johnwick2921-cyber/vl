export type Page =
  | 'agent'
  | 'traders'
  | 'trader'
  | 'strategy'
  | 'guide'
  | 'faq'
  | 'login'
  | 'register'
  | 'friends'

export const ROUTES = {
  home: '/',
  agent: '/agent',
  login: '/login',
  register: '/register',
  setup: '/setup',
  welcome: '/welcome',
  faq: '/faq',
  resetPassword: '/reset-password',
  settings: '/settings',
  traders: '/traders',
  dashboard: '/dashboard',
  strategy: '/strategy',
  guide: '/guide',
  friends: '/friends',
} as const

export const PAGE_PATHS: Record<Page, string> = {
  agent: ROUTES.agent,
  traders: ROUTES.traders,
  trader: ROUTES.dashboard,
  strategy: ROUTES.strategy,
  guide: ROUTES.guide,
  faq: ROUTES.faq,
  login: ROUTES.login,
  register: ROUTES.register,
  friends: ROUTES.friends,
}

export const LEGACY_HASH_ROUTES: Record<string, string> = {
  agent: ROUTES.agent,
  traders: ROUTES.traders,
  trader: ROUTES.dashboard,
  details: ROUTES.dashboard,
  strategy: ROUTES.strategy,
}

export function getCurrentPageForPath(pathname: string): Page | undefined {
  switch (pathname) {
    case ROUTES.agent:
      return 'agent'
    case ROUTES.welcome:
    case ROUTES.traders:
      return 'traders'
    case ROUTES.dashboard:
      return 'trader'
    case ROUTES.strategy:
      return 'strategy'
    case ROUTES.guide:
      return 'guide'
    case ROUTES.faq:
      return 'faq'
    case ROUTES.login:
      return 'login'
    case ROUTES.register:
      return 'register'
    case ROUTES.friends:
      return 'friends'
    default:
      return undefined
  }
}

export function buildDashboardPath(traderSlug?: string): string {
  if (!traderSlug) {
    return ROUTES.dashboard
  }

  return `${ROUTES.dashboard}?trader=${encodeURIComponent(traderSlug)}`
}

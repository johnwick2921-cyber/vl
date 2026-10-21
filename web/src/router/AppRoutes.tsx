import { type ReactNode, useEffect, useState } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import useSWR from 'swr'
import {
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
  useSearchParams,
} from 'react-router-dom'
import HeaderBar from '../components/common/HeaderBar'
import { SiteFooter } from '../components/common/SiteFooter'
import { LoginRequiredOverlay } from '../components/auth/LoginRequiredOverlay'
import { LoginPage } from '../components/auth/LoginPage'
import { RegisterPage } from '../components/auth/RegisterPage'
import { ResetPasswordPage } from '../components/auth/ResetPasswordPage'
import { SetupPage } from '../components/modals/SetupPage'
import { AITradersPage } from '../components/trader/AITradersPage'
import { AgentChatPage } from '../pages/AgentChatPage'
import { SettingsPage } from '../pages/SettingsPage'
import { FriendsPage } from '../pages/FriendsPage'
import { StrategyStudioPage } from '../pages/StrategyStudioPage'
import { TraderDashboardPage } from '../pages/TraderDashboardPage'
import { GuidePage } from '../guide/GuidePage'
import { useAuth } from '../contexts/AuthContext'
import {
  usePollResume,
  REFRESH_TRADERS_MS,
  REFRESH_TRADERS_DASH_MS,
  REFRESH_EXCHANGES_MS,
  REFRESH_STATUS_MS,
  REFRESH_ACCOUNT_MS,
  REFRESH_POSITIONS_MS,
  REFRESH_DECISIONS_MS,
  REFRESH_STATS_MS,
} from '../lib/autoRefresh'
import { useLanguage } from '../contexts/LanguageContext'
import { useSystemConfig } from '../hooks/useSystemConfig'
import { t } from '../i18n/translations'
import { api } from '../lib/api'
import type {
  AccountInfo,
  DecisionRecord,
  Exchange,
  Position,
  Statistics,
  SystemStatus,
  TraderInfo,
} from '../types'
import {
  buildDashboardPath,
  LEGACY_HASH_ROUTES,
  ROUTES,
  type Page,
} from './paths'
import { getTraderSlug } from './traderSlug'
import {
  resolveSelectedTrader,
  loadStoredTraderId,
  saveStoredTraderId,
  clearStoredTraderId,
} from './selectedTrader'

// getTraderSlug / findTraderBySlug live in ./traderSlug so the selection logic
// is unit-testable in isolation (see traderSlug.test.ts).

function LoadingScreen() {
  const { language } = useLanguage()

  return (
    <div
      className="min-h-screen flex items-center justify-center"
      style={{ background: '#0B0E11' }}
    >
      <div className="text-center">
        <img
          src="/icons/vl.svg"
          alt="VL"
          className="w-16 h-16 mx-auto mb-4 animate-pulse"
        />
        <p style={{ color: '#EAECEF' }}>{t('loading', language)}</p>
      </div>
    </div>
  )
}

function LegacyHashRedirect() {
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    const hashRoute = LEGACY_HASH_ROUTES[location.hash.slice(1)]
    if (!hashRoute) {
      return
    }

    if (hashRoute === location.pathname && location.hash === '') {
      return
    }

    navigate(
      {
        pathname: hashRoute,
        search: location.search,
      },
      { replace: true }
    )
  }, [location.hash, location.pathname, location.search, navigate])

  return null
}

interface AppChromeProps {
  children: ReactNode
  currentPage?: Page
  showFooter?: boolean
  wrapInMain?: boolean
  animateContent?: boolean
  extraContent?: ReactNode
}

function AppChrome({
  children,
  currentPage,
  showFooter = true,
  wrapInMain = true,
  animateContent = false,
  extraContent,
}: AppChromeProps) {
  const location = useLocation()
  const { language, setLanguage } = useLanguage()
  const { user, logout } = useAuth()
  const [loginOverlayOpen, setLoginOverlayOpen] = useState(false)
  const [loginOverlayFeature, setLoginOverlayFeature] = useState('')

  const handleLoginRequired = (featureName: string) => {
    setLoginOverlayFeature(featureName)
    setLoginOverlayOpen(true)
  }

  const content = animateContent ? (
    <AnimatePresence mode="wait">
      <motion.div
        key={`${location.pathname}${location.search}`}
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.15, ease: 'easeOut' }}
      >
        {children}
      </motion.div>
    </AnimatePresence>
  ) : (
    children
  )

  return (
    <div
      className="min-h-screen"
      style={{ background: '#0B0E11', color: '#EAECEF' }}
    >
      <HeaderBar
        isLoggedIn={!!user}
        currentPage={currentPage}
        language={language}
        onLanguageChange={setLanguage}
        user={user}
        onLogout={logout}
        onLoginRequired={handleLoginRequired}
      />

      {wrapInMain ? (
        <main className="min-h-screen pt-16">{content}</main>
      ) : (
        content
      )}

      {showFooter ? <SiteFooter language={language} /> : null}

      <LoginRequiredOverlay
        isOpen={loginOverlayOpen}
        onClose={() => setLoginOverlayOpen(false)}
        featureName={loginOverlayFeature}
      />

      {extraContent}
    </div>
  )
}

function TradersRoute() {
  const navigate = useNavigate()
  const { user, token } = useAuth()
  const { data: traders } = useSWR<TraderInfo[]>(
    user && token ? 'traders-route' : null,
    api.getTraders,
    {
      refreshInterval: REFRESH_TRADERS_MS,
      shouldRetryOnError: false,
    }
  )

  return (
    <AppChrome currentPage="traders" animateContent>
      <AITradersPage
        onTraderSelect={(traderId) => {
          const trader = traders?.find((item) => item.trader_id === traderId)
          navigate(
            buildDashboardPath(trader ? getTraderSlug(trader) : undefined)
          )
        }}
      />
    </AppChrome>
  )
}

function DashboardRoute() {
  const { language } = useLanguage()
  const { user, token } = useAuth()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedTraderSlug = searchParams.get('trader') || undefined
  const [selectedTraderId, setSelectedTraderId] = useState<string | undefined>()
  const [lastUpdate, setLastUpdate] = useState<string>('--:--:--')
  const [decisionsLimit, setDecisionsLimit] = useState(5)
  // Auto-resuming poll gates (was: permanent pollOff latches that froze the
  // surface until F5 — see lib/autoRefresh.ts). Failed-state UI is unchanged;
  // polling now self-heals after the backoff window.
  const accountGate = usePollResume()
  const positionsGate = usePollResume()
  const decisionsGate = usePollResume()
  const clearAccountGate = accountGate.clear
  const clearPositionsGate = positionsGate.clear
  const clearDecisionsGate = decisionsGate.clear

  useEffect(() => {
    clearAccountGate()
    clearPositionsGate()
    clearDecisionsGate()
  }, [selectedTraderId])

  const { data: traders, error: tradersError } = useSWR<TraderInfo[]>(
    user && token ? 'traders-dashboard' : null,
    () => api.getTraders(true),
    {
      refreshInterval: REFRESH_TRADERS_DASH_MS,
      shouldRetryOnError: false,
    }
  )

  const { data: exchanges } = useSWR<Exchange[]>(
    user && token ? 'exchanges-dashboard' : null,
    api.getExchangeConfigs,
    {
      refreshInterval: REFRESH_EXCHANGES_MS,
      shouldRetryOnError: false,
    }
  )

  // Selection is OWNER-OWNED and FIRST-CLASS. Priority (resolveSelectedTrader):
  // URL (?trader=, explicit View/bookmark) → in-memory → localStorage → a STABLE
  // first trader (never creation-order traders[0]). Every resolution is persisted
  // to BOTH the URL and localStorage, so the choice survives a poll, a header nav
  // that drops ?trader=, a refresh, a new tab, and a bookmark. The effect is
  // idempotent when the selection is stable — a valid selection sticks across
  // every poll cycle and never silently switches on a refresh/remount.
  useEffect(() => {
    if (!traders || traders.length === 0) {
      return
    }
    const storedId = loadStoredTraderId()
    const { trader, clearStored } = resolveSelectedTrader(
      traders,
      selectedTraderSlug,
      selectedTraderId,
      storedId
    )
    if (clearStored) {
      clearStoredTraderId() // a persisted id no longer resolves (deleted) → drop it
    }
    if (!trader) {
      return
    }
    if (trader.trader_id !== selectedTraderId) {
      setSelectedTraderId(trader.trader_id)
    }
    // Persist so a fresh load (new tab / bare /dashboard / bookmark) restores it.
    saveStoredTraderId(trader.trader_id)
    const canonical = getTraderSlug(trader)
    if (selectedTraderSlug !== canonical) {
      setSearchParams({ trader: canonical }, { replace: true })
    }
  }, [selectedTraderId, selectedTraderSlug, traders, setSearchParams])

  // Issue 2F — the currently selected NT account (from /api/accounts). Shares
  // the SWR cache with AccountSelector via the same key. Threaded into the
  // account-scoped keys + query params below so switching accounts fetches that
  // account's data instead of serving a trader-global cache entry.
  const { data: accountsData } = useSWR(
    selectedTraderId ? `accounts-${selectedTraderId}` : null,
    () => api.getAccounts(selectedTraderId as string),
    { revalidateOnFocus: false, dedupingInterval: 5000 }
  )
  const selectedAccount = accountsData?.current || ''
  const acctSuffix = selectedAccount ? `-${selectedAccount}` : ''

  const { data: status } = useSWR<SystemStatus>(
    selectedTraderId ? `status-${selectedTraderId}${acctSuffix}` : null,
    () => api.getStatus(selectedTraderId, true, selectedAccount),
    {
      refreshInterval: REFRESH_STATUS_MS,
      revalidateOnFocus: false,
      dedupingInterval: 3000,
    }
  )

  const { data: account } = useSWR<AccountInfo>(
    selectedTraderId ? `account-${selectedTraderId}${acctSuffix}` : null,
    () => api.getAccount(selectedTraderId, true, selectedAccount),
    {
      refreshInterval: accountGate.off ? 0 : REFRESH_ACCOUNT_MS,
      revalidateOnFocus: false,
      dedupingInterval: 3000,
      onErrorRetry: (_err, _key, _config, revalidate, { retryCount }) => {
        if (retryCount >= 2) {
          accountGate.latch() // pauses polling; auto-resumes after backoff
          return
        }
        setTimeout(() => revalidate({ retryCount }), 500)
      },
      onSuccess: () => {
        accountGate.clear()
      },
    }
  )

  const { data: positions } = useSWR<Position[]>(
    selectedTraderId ? `positions-${selectedTraderId}${acctSuffix}` : null,
    () => api.getPositions(selectedTraderId, true, selectedAccount),
    {
      refreshInterval: positionsGate.off ? 0 : REFRESH_POSITIONS_MS,
      revalidateOnFocus: false,
      dedupingInterval: 3000,
      onErrorRetry: (_err, _key, _config, revalidate, { retryCount }) => {
        if (retryCount >= 2) {
          positionsGate.latch() // pauses polling; auto-resumes after backoff
          return
        }
        setTimeout(() => revalidate({ retryCount }), 500)
      },
      onSuccess: () => {
        positionsGate.clear()
      },
    }
  )

  const { data: decisions } = useSWR<DecisionRecord[]>(
    selectedTraderId
      ? `decisions/latest-${selectedTraderId}-${decisionsLimit}${acctSuffix}`
      : null,
    () =>
      api.getLatestDecisions(
        selectedTraderId,
        decisionsLimit,
        true,
        selectedAccount
      ),
    {
      refreshInterval: decisionsGate.off ? 0 : REFRESH_DECISIONS_MS,
      revalidateOnFocus: false,
      dedupingInterval: 3000,
      onErrorRetry: (_err, _key, _config, revalidate, { retryCount }) => {
        if (retryCount >= 2) {
          decisionsGate.latch() // pauses polling; auto-resumes after backoff
          return
        }
        setTimeout(() => revalidate({ retryCount }), 500)
      },
      onSuccess: () => {
        decisionsGate.clear()
      },
    }
  )

  const { data: stats } = useSWR<Statistics>(
    selectedTraderId ? `statistics-${selectedTraderId}${acctSuffix}` : null,
    () => api.getStatistics(selectedTraderId, true, selectedAccount),
    {
      refreshInterval: REFRESH_STATS_MS,
      revalidateOnFocus: false,
      dedupingInterval: 20000,
    }
  )

  useEffect(() => {
    if (account) {
      setLastUpdate(new Date().toLocaleTimeString())
    }
  }, [account])

  const selectedTrader = traders?.find(
    (trader) => trader.trader_id === selectedTraderId
  )

  return (
    <AppChrome currentPage="trader" animateContent>
      <TraderDashboardPage
        selectedTrader={selectedTrader}
        status={status}
        account={account}
        accountFailed={accountGate.off}
        selectedAccount={selectedAccount}
        positions={positions}
        positionsFailed={positionsGate.off}
        decisions={decisions}
        decisionsFailed={decisionsGate.off}
        decisionsLimit={decisionsLimit}
        onDecisionsLimitChange={setDecisionsLimit}
        stats={stats}
        lastUpdate={lastUpdate}
        language={language}
        traders={traders}
        tradersError={tradersError}
        selectedTraderId={selectedTraderId}
        onTraderSelect={(traderId) => {
          setSelectedTraderId(traderId)
          const trader = traders?.find((item) => item.trader_id === traderId)
          navigate(
            buildDashboardPath(trader ? getTraderSlug(trader) : undefined),
            {
              replace: true,
            }
          )
        }}
        onNavigateToTraders={() => navigate(ROUTES.traders)}
        exchanges={exchanges}
      />
    </AppChrome>
  )
}

export function AppRoutes() {
  const { user, token, isLoading } = useAuth()
  const { config: systemConfig, loading: configLoading } = useSystemConfig()
  const isAuthenticated = !!user && !!token

  if (isLoading || configLoading) {
    return <LoadingScreen />
  }

  if (systemConfig && !systemConfig.initialized && !user) {
    return <SetupPage />
  }

  return (
    <>
      <LegacyHashRedirect />
      <Routes>
        <Route
          path={ROUTES.home}
          element={
            user ? (
              <Navigate to={ROUTES.settings} replace />
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route path={ROUTES.login} element={<LoginPage />} />
        <Route path={ROUTES.register} element={<RegisterPage />} />
        <Route path={ROUTES.resetPassword} element={<ResetPasswordPage />} />
        <Route
          path={ROUTES.setup}
          element={
            user ? (
              <Navigate to={ROUTES.welcome} replace />
            ) : systemConfig?.initialized ? (
              <Navigate to={ROUTES.login} replace />
            ) : (
              <SetupPage />
            )
          }
        />
        {/* UI-GUIDE-TRUTH LF-1 (release #22): /faq used to render the
            inherited crypto/Docker FAQ, incl. "pkill vl" and deleting
            data.db-wal as "fixes" (a silent no-relaunch and lost writes).
            The in-app Guide is the one FAQ; the old URL lands there. */}
        <Route
          path={ROUTES.faq}
          element={<Navigate to={ROUTES.guide} replace />}
        />
        <Route
          path={ROUTES.agent}
          element={
            <AppChrome currentPage="agent" showFooter={false}>
              <AgentChatPage />
            </AppChrome>
          }
        />
        <Route
          path={ROUTES.settings}
          element={
            isAuthenticated ? (
              <AppChrome showFooter={false}>
                <SettingsPage />
              </AppChrome>
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.welcome}
          element={
            isAuthenticated ? (
              <Navigate to={ROUTES.traders} replace />
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.traders}
          element={
            isAuthenticated ? (
              <TradersRoute />
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.dashboard}
          element={
            isAuthenticated ? (
              <DashboardRoute />
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.strategy}
          element={
            isAuthenticated ? (
              <AppChrome currentPage="strategy" animateContent>
                <StrategyStudioPage />
              </AppChrome>
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.guide}
          element={
            isAuthenticated ? (
              <AppChrome currentPage="guide" animateContent>
                <GuidePage />
              </AppChrome>
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route
          path={ROUTES.friends}
          element={
            isAuthenticated ? (
              <AppChrome currentPage="friends" showFooter={false}>
                <FriendsPage />
              </AppChrome>
            ) : (
              <Navigate to={ROUTES.login} replace />
            )
          }
        />
        <Route path="*" element={<Navigate to={ROUTES.home} replace />} />
      </Routes>
    </>
  )
}

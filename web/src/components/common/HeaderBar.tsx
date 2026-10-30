import { PERSONA_NAME } from '../../constants/branding'
import { useState, useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import { Menu, X, ChevronDown, Settings } from 'lucide-react'
import { UpdateBadge } from '../updates/UpdateBadge'
import { t, type Language } from '../../i18n/translations'
import { StatusBanner } from './StatusBanner'
import { DataPausedBanner } from './DataPausedBanner'
import { getCurrentPageForPath, ROUTES, type Page } from '../../router/paths'
import { useFriendsPageEnabled } from '../../hooks/useFriendsPageEnabled'

interface HeaderBarProps {
  onLoginClick?: () => void
  isLoggedIn?: boolean
  isHomePage?: boolean
  currentPage?: Page
  language?: Language
  onLanguageChange?: (lang: Language) => void
  user?: { email: string } | null
  onLogout?: () => void
  onPageChange?: (page: Page) => void
  onLoginRequired?: (featureName: string) => void
}

export default function HeaderBar({
  isLoggedIn = false,
  isHomePage = false,
  currentPage,
  language = 'zh' as Language,
  onLanguageChange,
  user,
  onLogout,
  onPageChange,
  onLoginRequired,
}: HeaderBarProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [languageDropdownOpen, setLanguageDropdownOpen] = useState(false)
  const [userDropdownOpen, setUserDropdownOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const userDropdownRef = useRef<HTMLDivElement>(null)
  // m386: the Friends nav entry is hidden unless VL_FRIENDS_PAGE=1 (the flag
  // comes from /api/health; false while unknown).
  const friendsPageEnabled = useFriendsPageEnabled()
  const resolvedCurrentPage =
    currentPage ?? getCurrentPageForPath(location.pathname)

  const navigateInApp = (path: string) => {
    navigate(path)
  }

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(event.target as Node)
      ) {
        setLanguageDropdownOpen(false)
      }
      if (
        userDropdownRef.current &&
        !userDropdownRef.current.contains(event.target as Node)
      ) {
        setUserDropdownOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [])

  return (
    <nav className="fixed top-0 w-full z-50 header-bar">
      <div className="flex items-center justify-between h-16 px-4 sm:px-6 max-w-[1920px] mx-auto">
        {/* Logo - Always go to home page */}
        <div
          onClick={() => {
            navigateInApp(ROUTES.home)
          }}
          className="flex items-center gap-2 hover:opacity-80 transition-opacity cursor-pointer"
        >
          <img src="/icons/vl.svg" alt="VL" className="w-7 h-7" />
          <span className="text-lg font-bold text-vl-neo-gold">
            {PERSONA_NAME}
          </span>
        </div>

        {/* Desktop Menu */}
        <div className="hidden md:flex items-center justify-between flex-1 ml-8">
          {/* Left Side - Navigation Tabs - Always show all tabs */}
          <div className="flex items-center gap-2">
            {/* Navigation tabs configuration */}
            {(() => {
              // Define all navigation tabs
              const navTabs: {
                page: Page
                path: string
                label: string
                requiresAuth: boolean
                badge?: string
                hidden?: boolean
              }[] = [
                {
                  page: 'agent',
                  path: ROUTES.agent,
                  label: 'Agent',
                  badge: 'Beta',
                  requiresAuth: false,
                },
                {
                  page: 'traders',
                  path: ROUTES.traders,
                  label: t('configNav', language),
                  requiresAuth: true,
                },
                {
                  page: 'trader',
                  path: ROUTES.dashboard,
                  label: t('dashboardNav', language),
                  requiresAuth: true,
                },
                {
                  page: 'strategy',
                  path: ROUTES.strategy,
                  label: t('strategyNav', language),
                  requiresAuth: true,
                },
                {
                  page: 'guide',
                  path: ROUTES.guide,
                  label: '📖 Guide',
                  requiresAuth: true,
                },
                ...(friendsPageEnabled
                  ? [
                      {
                        page: 'friends' as const,
                        path: ROUTES.friends,
                        label: t('friendsNav', language),
                        requiresAuth: true,
                      },
                    ]
                  : []),
              ]

              const handleNavClick = (tab: (typeof navTabs)[0]) => {
                // If requires auth and not logged in, show login prompt
                if (tab.requiresAuth && !isLoggedIn) {
                  onLoginRequired?.(tab.label)
                  return
                }
                // Navigate normally
                if (onPageChange) {
                  onPageChange(tab.page)
                }
                navigateInApp(tab.path)
              }

              return navTabs
                .filter((tab) => !tab.hidden)
                .map((tab) => (
                  <button
                    key={tab.page}
                    onClick={() => handleNavClick(tab)}
                    className={`text-sm font-bold transition-all duration-300 relative focus:outline-2 focus:outline-yellow-500 px-3 py-2 rounded-lg
                    ${resolvedCurrentPage === tab.page ? 'text-vl-neo-gold' : 'text-vl-neo-text-muted hover:text-vl-neo-gold'}`}
                  >
                    {resolvedCurrentPage === tab.page && (
                      <span className="absolute inset-0 rounded-lg bg-vl-neo-gold/15 -z-10" />
                    )}
                    {tab.label}
                    {tab.badge && (
                      <span className="ml-1 text-[10px] px-1.5 py-0.5 rounded-full bg-vl-neo-gold/20 text-vl-neo-gold font-semibold uppercase align-top relative -top-1">
                        {tab.badge}
                      </span>
                    )}
                  </button>
                ))
            })()}
          </div>

          {/* Right Side - User Actions */}
          <div className="flex items-center gap-4">
            {isLoggedIn && user && <UpdateBadge />}
            {/* User Info and Actions */}
            {isLoggedIn && user ? (
              <div className="flex items-center gap-3">
                {/* User Info with Dropdown */}
                <div className="relative" ref={userDropdownRef}>
                  <button
                    onClick={() => setUserDropdownOpen(!userDropdownOpen)}
                    className="flex items-center gap-2 px-3 py-2 rounded transition-colors bg-vl-neo-bg-lighter border border-vl-neo-gold/20 hover:bg-white/5"
                  >
                    <div className="w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold bg-vl-neo-gold text-black">
                      {user.email[0].toUpperCase()}
                    </div>
                    <span className="text-sm text-vl-neo-text-muted">
                      {user.email}
                    </span>
                    <ChevronDown className="w-4 h-4 text-vl-neo-text-muted" />
                  </button>

                  {userDropdownOpen && (
                    <div className="absolute right-0 top-full mt-2 w-48 rounded-lg shadow-lg overflow-hidden z-50 bg-vl-neo-bg-lighter border border-vl-neo-gold/20">
                      <div className="px-3 py-2 border-b border-vl-neo-gold/20">
                        <div className="text-xs text-vl-neo-text-muted">
                          {t('loggedInAs', language)}
                        </div>
                        <div className="text-sm font-medium text-vl-neo-text-muted">
                          {user.email}
                        </div>
                      </div>
                      <button
                        onClick={() => {
                          navigateInApp(ROUTES.settings)
                          setUserDropdownOpen(false)
                        }}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm transition-colors hover:bg-white/5 text-vl-neo-text-muted hover:text-white"
                      >
                        <Settings className="w-3.5 h-3.5" />
                        Settings
                      </button>
                      {onLogout && (
                        <button
                          onClick={() => {
                            onLogout()
                            setUserDropdownOpen(false)
                          }}
                          className="w-full px-3 py-2 text-sm font-semibold transition-colors hover:opacity-80 text-center bg-vl-neo-danger/20 text-vl-neo-danger"
                        >
                          {t('exitLogin', language)}
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </div>
            ) : (
              /* Show login/register buttons when not logged in and not on login/register pages */
              resolvedCurrentPage !== 'login' &&
              resolvedCurrentPage !== 'register' && (
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    onClick={() => navigateInApp(ROUTES.login)}
                    className="px-3 py-2 text-sm font-medium transition-colors rounded text-vl-neo-text-muted hover:text-white"
                  >
                    {t('signIn', language)}
                  </button>
                </div>
              )
            )}

            {/* Language Toggle - Always at the rightmost */}
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => setLanguageDropdownOpen(!languageDropdownOpen)}
                className="flex items-center gap-2 px-3 py-2 rounded transition-colors text-vl-neo-text-muted hover:bg-white/5"
              >
                <span className="text-lg">
                  {language === 'zh' ? '🇨🇳' : language === 'id' ? '🇮🇩' : '🇺🇸'}
                </span>
                <ChevronDown className="w-4 h-4" />
              </button>

              {languageDropdownOpen && (
                <div className="absolute right-0 top-full mt-2 w-32 rounded-lg shadow-lg overflow-hidden z-50 bg-vl-neo-bg-lighter border border-vl-neo-gold/20">
                  <button
                    onClick={() => {
                      onLanguageChange?.('zh')
                      setLanguageDropdownOpen(false)
                    }}
                    className={`w-full flex items-center gap-2 px-3 py-2 transition-colors text-vl-neo-text-muted hover:text-white
                      ${language === 'zh' ? 'bg-vl-neo-gold/10' : 'hover:bg-white/5'}`}
                  >
                    <span className="text-base">🇨🇳</span>
                    <span className="text-sm">中文</span>
                  </button>
                  <button
                    onClick={() => {
                      onLanguageChange?.('en')
                      setLanguageDropdownOpen(false)
                    }}
                    className={`w-full flex items-center gap-2 px-3 py-2 transition-colors text-vl-neo-text-muted hover:text-white
                      ${language === 'en' ? 'bg-vl-neo-gold/10' : 'hover:bg-white/5'}`}
                  >
                    <span className="text-base">🇺🇸</span>
                    <span className="text-sm">English</span>
                  </button>
                  <button
                    onClick={() => {
                      onLanguageChange?.('id')
                      setLanguageDropdownOpen(false)
                    }}
                    className={`w-full flex items-center gap-2 px-3 py-2 transition-colors text-vl-neo-text-muted hover:text-white
                      ${language === 'id' ? 'bg-vl-neo-gold/10' : 'hover:bg-white/5'}`}
                  >
                    <span className="text-base">🇮🇩</span>
                    <span className="text-sm">Bahasa</span>
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Mobile Menu Button */}
        <motion.button
          onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
          className="md:hidden text-vl-neo-text-muted hover:text-white"
          whileTap={{ scale: 0.9 }}
        >
          {mobileMenuOpen ? (
            <X className="w-6 h-6" />
          ) : (
            <Menu className="w-6 h-6" />
          )}
        </motion.button>
      </div>

      {/* m420a/B6 — CME-closed / feed-down line. It sits in the <nav> itself,
          so the one mount shows under the header on desktop AND mobile (the
          mobile overlay below starts at top:64px, under this nav's z-50). */}
      <StatusBanner language={language} />
      {/* m445 — TRADING PAUSED for missing bars (data-integrity gate) */}
      <DataPausedBanner language={language} />

      {/* Mobile Menu Overlay */}
      <AnimatePresence>
        {mobileMenuOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-40 md:hidden bg-black/90 backdrop-blur-xl"
            style={{ top: '64px' }} // Below header
          >
            <motion.div
              initial={{ y: -20, opacity: 0 }}
              animate={{ y: 0, opacity: 1 }}
              transition={{ delay: 0.1, duration: 0.3 }}
              className="flex flex-col h-[calc(100vh-64px)] overflow-y-auto px-6 py-8"
            >
              {/* Navigation Links */}
              <div className="flex flex-col gap-6 mb-12">
                {(() => {
                  const navTabs: {
                    page: Page
                    path: string
                    label: string
                    requiresAuth: boolean
                    badge?: string
                    hidden?: boolean
                  }[] = [
                    {
                      page: 'agent',
                      path: ROUTES.agent,
                      label: 'Agent',
                      requiresAuth: false,
                    },
                    {
                      page: 'traders',
                      path: ROUTES.traders,
                      label: t('configNav', language),
                      requiresAuth: true,
                    },
                    {
                      page: 'trader',
                      path: ROUTES.dashboard,
                      label: t('dashboardNav', language),
                      requiresAuth: true,
                    },
                    {
                      page: 'strategy',
                      path: ROUTES.strategy,
                      label: t('strategyNav', language),
                      requiresAuth: true,
                    },
                    {
                      page: 'guide',
                      path: ROUTES.guide,
                      label: '📖 Guide',
                      requiresAuth: true,
                    },
                    ...(friendsPageEnabled
                      ? [
                          {
                            page: 'friends' as const,
                            path: ROUTES.friends,
                            label: t('friendsNav', language),
                            requiresAuth: true,
                          },
                        ]
                      : []),
                  ]

                  const handleMobileNavClick = (tab: (typeof navTabs)[0]) => {
                    if (tab.requiresAuth && !isLoggedIn) {
                      onLoginRequired?.(tab.label)
                      setMobileMenuOpen(false)
                      return
                    }
                    if (onPageChange) {
                      onPageChange(tab.page)
                    }
                    navigateInApp(tab.path)
                    setMobileMenuOpen(false)
                  }

                  return navTabs
                    .filter((tab) => !tab.hidden)
                    .map((tab, i) => (
                      <motion.button
                        key={tab.page}
                        initial={{ x: -20, opacity: 0 }}
                        animate={{ x: 0, opacity: 1 }}
                        transition={{ delay: 0.1 + i * 0.05 }}
                        onClick={() => handleMobileNavClick(tab)}
                        className={`text-2xl font-black tracking-tight text-left flex items-center gap-3
                        ${resolvedCurrentPage === tab.page ? 'text-vl-neo-gold' : 'text-zinc-500'}`}
                      >
                        {resolvedCurrentPage === tab.page && (
                          <motion.div
                            layoutId="active-indicator"
                            className="w-1.5 h-1.5 rounded-full bg-vl-neo-gold"
                          />
                        )}
                        {tab.label}
                        {tab.badge && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-vl-neo-gold/20 text-vl-neo-gold font-semibold uppercase align-middle relative -top-1">
                            {tab.badge}
                          </span>
                        )}
                        {tab.requiresAuth && !isLoggedIn && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded border border-zinc-800 text-zinc-500 font-normal tracking-wide uppercase align-middle relative -top-1">
                            LOGIN_REQ
                          </span>
                        )}
                      </motion.button>
                    ))
                })()}

                {/* Original Page Links */}
                {isHomePage && (
                  <div className="pt-6 border-t border-white/5 space-y-4">
                    {[
                      { key: 'features', label: t('features', language) },
                      { key: 'howItWorks', label: t('howItWorks', language) },
                    ].map((item, i) => (
                      <motion.a
                        key={item.key}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: 0.5 + i * 0.1 }}
                        href={`#${item.key === 'features' ? 'features' : 'how-it-works'}`}
                        className="block text-lg font-mono text-zinc-600 hover:text-white"
                        onClick={() => setMobileMenuOpen(false)}
                      >
                        {'>'} {item.label}
                      </motion.a>
                    ))}
                  </div>
                )}
              </div>

              {/* Bottom Actions */}
              <div className="mt-auto space-y-8">
                {/* Account / Lang */}
                <div className="grid grid-cols-2 gap-4">
                  {/* Lang Switcher */}
                  <div className="flex bg-zinc-900 rounded-lg p-1 border border-zinc-800">
                    {['zh', 'en', 'id'].map((lang) => (
                      <button
                        key={lang}
                        onClick={() => {
                          onLanguageChange?.(lang as Language)
                          setMobileMenuOpen(false)
                        }}
                        className={`flex-1 py-3 text-sm font-bold rounded-md transition-colors ${
                          language === lang
                            ? 'bg-zinc-800 text-white shadow-sm'
                            : 'text-zinc-500'
                        }`}
                      >
                        {lang === 'zh' ? 'CN' : lang === 'id' ? 'ID' : 'EN'}
                      </button>
                    ))}
                  </div>

                  {/* Auth Actions */}
                  {isLoggedIn && user ? (
                    <button
                      onClick={() => {
                        onLogout?.()
                        setMobileMenuOpen(false)
                      }}
                      className="bg-red-500/10 border border-red-500/20 text-red-500 rounded-lg font-bold text-sm hover:bg-red-500/20 transition-colors"
                    >
                      {t('exitLogin', language)}
                    </button>
                  ) : (
                    resolvedCurrentPage !== 'login' &&
                    resolvedCurrentPage !== 'register' && (
                      <button
                        type="button"
                        onClick={() => {
                          navigateInApp(ROUTES.login)
                          setMobileMenuOpen(false)
                        }}
                        className="flex items-center justify-center bg-vl-neo-gold text-black rounded-lg font-bold text-sm hover:bg-yellow-400 transition-colors"
                      >
                        {t('signIn', language)}
                      </button>
                    )
                  )}
                </div>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </nav>
  )
}

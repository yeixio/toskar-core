import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { useRole } from '@/lib/role'
import { matches, settingsNav, visibleTo, type SettingsNavItem } from './settingsNav'

/**
 * Settings (#1374, #1321): a list of categories beside the open one. Wide,
 * the list stays in view; at phone width it is its own page, and each
 * category has a way back to it, as on the phone.
 */
export function SettingsLayout() {
  const { t } = useTranslation('settings')
  const { pathname } = useLocation()
  const { role, atLeast } = useRole()
  const [query, setQuery] = useState('')
  const atIndex = pathname.replace(/\/+$/, '') === '/settings'
  const known = role !== undefined
  // A keyword may name another namespace's key, as apiAccess:lan.title.
  const label = (key: string) => t(key.includes(':') ? key : `settings:${key}`)
  const show = (item: SettingsNavItem) => visibleTo(item.min, atLeast, known) && matches(item, query, label)

  const groups = settingsNav
    .filter((group) => visibleTo(group.min, atLeast, known))
    .map((group) => ({
      ...group,
      categories: group.categories
        .map((category) => ({ ...category, items: category.items.filter(show) }))
        .filter((category) => category.items.length > 0),
    }))
    .filter((group) => group.categories.length > 0)

  return (
    <div className="mx-auto flex w-full max-w-5xl min-w-0 gap-8">
      <nav
        aria-label={t('nav.label')}
        className={[atIndex ? 'block w-full' : 'hidden', 'md:block md:w-56 md:shrink-0'].join(' ')}
      >
        {/* Wide, the open category's title is the page's heading. */}
        {atIndex ? <h1 className="page-title mb-4 md:hidden">{t('page.title')}</h1> : null}
        <label className="mb-4 block">
          <span className="sr-only">{t('nav.search')}</span>
          <input
            type="search"
            className="field w-full"
            placeholder={t('nav.search')}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </label>
        {groups.length === 0 ? <p className="text-sm text-ink-muted">{t('nav.searchEmpty')}</p> : null}
        <div className="space-y-5">
          {groups.map((group) => (
            <div key={group.label}>
              <p className="label-caps mb-1 px-2.5">{t(group.label)}</p>
              <ul className="flex flex-col gap-0.5">
                {group.categories.map((category, i) => (
                  <li key={category.label ?? `${group.label}-${i}`}>
                    {category.label ? (
                      <p className="mt-2 px-2.5 pb-0.5 text-xs font-medium text-ink-faint">{t(category.label)}</p>
                    ) : null}
                    <ul className="flex flex-col gap-0.5">
                      {category.items.map((item) => (
                        <li key={item.path}>
                          <NavLink
                            to={`/settings/${item.path}`}
                            className={({ isActive }) =>
                              ['nav-link', category.label ? 'ms-3' : '', isActive || (atIndex && item.path === 'account') ? 'nav-link-active' : '']
                                .filter(Boolean)
                                .join(' ')
                            }
                          >
                            {t(item.label)}
                          </NavLink>
                        </li>
                      ))}
                    </ul>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </nav>
      <div className={[atIndex ? 'hidden md:block' : 'block', 'min-w-0 flex-1'].join(' ')}>
        {atIndex ? null : (
          <Link to="/settings" className="mb-3 inline-flex items-center gap-1 text-sm text-primary md:hidden">
            <span aria-hidden className="rtl:rotate-180">‹</span>
            {t('nav.back')}
          </Link>
        )}
        <Outlet />
      </div>
    </div>
  )
}

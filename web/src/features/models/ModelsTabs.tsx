import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'

/** Models and Train your own, as two pages of one place (#1374). */
export function ModelsTabs() {
  const { t } = useTranslation()
  return (
    <nav aria-label={t('nav.models')} className="segmented">
      <NavLink to="/models" end className="segmented-item">
        {t('nav.models')}
      </NavLink>
      <NavLink to="/models/train" className="segmented-item">
        {t('nav.train')}
      </NavLink>
    </nav>
  )
}

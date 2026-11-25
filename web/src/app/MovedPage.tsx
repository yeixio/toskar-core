import { Navigate, useLocation } from 'react-router-dom'

/** A page that moved (#1374): its old link opens the new place, with the same query and anchor. */
export function MovedPage({ to }: { to: string }) {
  const { search, hash } = useLocation()
  return <Navigate to={`${to}${search}${hash}`} replace />
}

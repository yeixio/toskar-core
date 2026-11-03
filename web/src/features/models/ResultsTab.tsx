import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatDecimal, formatPercent } from '@/i18n/format'
import { api } from '@/lib/api'
import { compareAlphabetical, overallScore } from './resultsSort'
import type { QualityResult } from '@/types/api'

/** A set's name, when this build has one; a new set still shows, by its key. */
const KNOWN_SUITES = ['quality', 'topics', 'files', 'reasoning', 'triage', 'fix_test']

const METHOD_URL = 'https://github.com/yeixio/toskar-core/blob/main/docs/quality.md'
const SUGGEST_URL = 'https://github.com/yeixio/toskar-core/issues/new?template=quality_suggestion.yml'
const CONTRIBUTE_URL = `${METHOD_URL}#add-your-computers-results`

type SortKey = 'name' | 'hardware' | 'date' | 'method_version' | 'overall' | 'runs' | 'writes' | 'reads' | 'first_token' | `suite:${string}`

function valueOf(row: QualityResult, key: SortKey): string | number | null {
  if (key === 'name' || key === 'hardware' || key === 'date') return row[key]
  if (key === 'method_version') return row.method_version
  if (key === 'overall') return overallScore(row.scores)
  if (key === 'runs') return row.runs ?? null
  if (key === 'writes') return row.performance?.tokens_per_second ?? null
  if (key === 'reads') return row.performance?.prompt_tokens_per_second ?? null
  if (key === 'first_token') return row.performance?.first_token_ms ?? null
  const score = row.scores[key.slice('suite:'.length)]
  return score ? score.passed / score.of : null
}

/** What's been measured so far, to sort through (#593). It draws no conclusions. */
export function ResultsTab() {
  const { t } = useTranslation('models')
  const [sort, setSort] = useState<{ key: SortKey; descending: boolean }>({ key: 'date', descending: true })
  const [computer, setComputer] = useState('')
  const query = useQuery({
    queryKey: ['quality-results'],
    queryFn: () => api.getQualityResults(),
    retry: false,
    staleTime: 10 * 60_000,
  })
  const results = useMemo(() => query.data?.results ?? [], [query.data])
  const computers = useMemo(() => [...new Set(results.map((r) => r.hardware))].sort(), [results])
  const suites = useMemo(() => {
    const found = new Set(results.flatMap((r) => Object.keys(r.scores)))
    return [...KNOWN_SUITES.filter((s) => found.has(s)), ...[...found].filter((s) => !KNOWN_SUITES.includes(s))]
  }, [results])
  const suiteName = (suite: string) => (KNOWN_SUITES.includes(suite) ? t(`results.suites.${suite}`) : suite)

  const rows = useMemo(() => {
    const shown = computer ? results.filter((r) => r.hardware === computer) : [...results]
    return shown.sort((a, b) => {
      const x = valueOf(a, sort.key)
      const y = valueOf(b, sort.key)
      // A row with no score for this column goes last, whichever way it's sorted.
      if (x === null) return y === null ? 0 : 1
      if (y === null) return -1
      const order = typeof x === 'number' && typeof y === 'number' ? x - y : compareAlphabetical(String(x), String(y))
      return sort.descending ? -order : order
    })
  }, [results, computer, sort])

  const heading = (key: SortKey, label: string) => {
    const active = sort.key === key
    return (
      <th scope="col" aria-sort={active ? (sort.descending ? 'descending' : 'ascending') : 'none'} className="whitespace-nowrap px-3 py-2 text-start font-medium">
        <button
          type="button"
          className="hover:text-ink"
          onClick={() => setSort({ key, descending: active ? !sort.descending : key !== 'name' && key !== 'hardware' && key !== 'first_token' })}
        >
          {label}
          {active ? <span aria-hidden> {sort.descending ? '↓' : '↑'}</span> : null}
        </button>
      </th>
    )
  }

  if (query.isLoading) return <p className="text-sm text-ink-muted">{t('results.loading')}</p>
  if (!query.data?.live) return <p className="text-sm text-ink-muted">{t('results.offline')}</p>

  const versions = new Set(rows.map((r) => r.method_version))
  const machines = new Set(rows.map((r) => r.hardware))
  const none = <span className="text-ink-muted">–</span>
  const speed = (tokens?: number) => (tokens ? formatDecimal(tokens, tokens >= 100 ? 0 : 1) : none)

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h2 className="section-title">{t('results.title')}</h2>
        <p className="text-sm text-ink-muted">{t('results.intro')}</p>
      </div>
      <p className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        <a className="text-primary underline-offset-2 hover:underline" href={METHOD_URL} target="_blank" rel="noreferrer noopener">
          {t('results.method')}
        </a>
        <a className="text-primary underline-offset-2 hover:underline" href={SUGGEST_URL} target="_blank" rel="noreferrer noopener">
          {t('results.suggest')}
        </a>
        <a className="text-primary underline-offset-2 hover:underline" href={CONTRIBUTE_URL} target="_blank" rel="noreferrer noopener">
          {t('results.contribute')}
        </a>
      </p>
      {computers.length > 1 ? (
        <label className="flex flex-wrap items-center gap-2 text-sm text-ink-muted">
          {t('results.computer')}
          <select className="field w-auto" value={computer} onChange={(e) => setComputer(e.target.value)}>
            <option value="">{t('results.allComputers')}</option>
            {computers.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      {machines.size > 1 || versions.size > 1 ? <p className="text-sm text-ink-muted">{t('results.notSameTest')}</p> : null}
      <div className="overflow-x-auto rounded-xl border border-line">
        <table className="w-full text-sm">
          <thead className="text-ink-muted">
            <tr className="border-b border-line">
              {heading('name', t('results.model'))}
              {heading('runs', t('results.runs'))}
              {heading('overall', t('results.overall'))}
              {suites.map((s) => heading(`suite:${s}`, suiteName(s)))}
              {heading('writes', t('results.writes'))}
              {heading('reads', t('results.reads'))}
              {heading('first_token', t('results.firstToken'))}
              {heading('hardware', t('results.computer'))}
              {heading('date', t('results.date'))}
              {heading('method_version', t('results.version'))}
              <th scope="col" className="whitespace-nowrap px-3 py-2 text-start font-medium">
                {t('results.source')}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.model}-${r.hardware}-${r.date}`} className="border-b border-line last:border-0">
                <th scope="row" className="whitespace-nowrap px-3 py-2 text-start font-medium text-ink">
                  {r.name}
                  {r.submitted ? (
                    <span className="ms-2 text-xs font-normal text-ink-muted">{r.submitted_by || t('results.community')}</span>
                  ) : null}
                </th>
                <td className="whitespace-nowrap px-3 py-2">{r.runs ? t(`results.runsValue.${r.runs}`) : none}</td>
                <td className="whitespace-nowrap px-3 py-2 font-medium tabular-nums">
                  {overallScore(r.scores) === null ? none : t('results.overallValue', { score: overallScore(r.scores), version: r.method_version })}
                </td>
                {suites.map((s) => {
                  const score = r.scores[s]
                  return (
                    <td key={s} className="whitespace-nowrap px-3 py-2 tabular-nums" title={score ? formatPercent(score.passed / score.of) : undefined}>
                      {score ? t('results.score', { passed: score.passed, of: score.of }) : <span className="text-ink-faint">–</span>}
                    </td>
                  )
                })}
                <td className="whitespace-nowrap px-3 py-2 tabular-nums">{speed(r.performance?.tokens_per_second)}</td>
                <td className="whitespace-nowrap px-3 py-2 tabular-nums">{speed(r.performance?.prompt_tokens_per_second)}</td>
                <td className="whitespace-nowrap px-3 py-2 tabular-nums">
                  {r.performance ? t('results.seconds', { seconds: formatDecimal(r.performance.first_token_ms / 1000, 1) }) : none}
                </td>
                <td className="whitespace-nowrap px-3 py-2">{r.hardware}</td>
                <td className="whitespace-nowrap px-3 py-2">{formatDate(`${r.date}T12:00:00`)}</td>
                <td className="whitespace-nowrap px-3 py-2 tabular-nums">{r.method_version}</td>
                <td className="whitespace-nowrap px-3 py-2">
                  {r.source ? (
                    <a className="text-primary underline-offset-2 hover:underline" href={r.source} target="_blank" rel="noreferrer noopener">
                      {t('results.sourceLink')}
                    </a>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

import { useState } from 'react'
import { ActivityPanel } from './ActivityPanel'
import { BenchmarkPanel } from './BenchmarkPanel'
import { OverviewPanel } from './OverviewPanel'
import { RealmKicker } from '@/components/ui/Realm'

type Tab = 'overview' | 'benchmarks' | 'activity'

const tabs: { id: Tab; label: string }[] = [
  { id: 'overview', label: 'Overview' },
  { id: 'benchmarks', label: 'Benchmarks' },
  { id: 'activity', label: 'Activity' },
]

export function PerformancePage() {
  const [tab, setTab] = useState<Tab>('overview')

  return (
    <div className="w-full min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">Performance</h1>
        <p className="page-subtitle">
          What your AI is doing right now — load, speed, and how work moves across your team.
        </p>
      </header>

      <div className="flex flex-wrap gap-2" role="tablist" aria-label="Performance views">
        {tabs.map((option) => (
          <button
            key={option.id}
            type="button"
            role="tab"
            aria-selected={tab === option.id}
            onClick={() => setTab(option.id)}
            className={[
              'rounded-lg border px-4 py-2 text-sm font-medium transition',
              tab === option.id
                ? 'border-primary bg-primary-soft text-primary-active'
                : 'border-line bg-surface text-ink-muted hover:border-primary/40 hover:text-ink',
            ].join(' ')}
          >
            {option.label}
          </button>
        ))}
      </div>

      {tab === 'overview' && <OverviewPanel />}
      {tab === 'benchmarks' && <BenchmarkPanel />}
      {tab === 'activity' && <ActivityPanel />}
    </div>
  )
}

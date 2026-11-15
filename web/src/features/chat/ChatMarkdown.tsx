import { useMemo } from 'react'
import type { Components } from 'react-markdown'
import ReactMarkdown from 'react-markdown'
import { useTranslation } from 'react-i18next'
import remarkGfm from 'remark-gfm'
import { prepareChatMarkdown } from './displayChatText'

/** What asking each linked site found (#209, contract 1.97). */
export type LinkCheck = { dead?: string[]; unknown?: string[] }

/** A link as compared: decoded, without a trailing slash. */
function sameLink(href: string): string {
  let s = href.trim()
  try {
    s = decodeURI(s)
  } catch {
    // An address that won't decode is compared as written.
  }
  return s.replace(/\/+$/, '')
}

function statusOf(href: string, check?: LinkCheck): 'dead' | 'unknown' | null {
  if (!check) return null
  const key = sameLink(href)
  if (check.dead?.some((l) => sameLink(l) === key)) return 'dead'
  if (check.unknown?.some((l) => sameLink(l) === key)) return 'unknown'
  return null
}

const baseComponents: Components = {
  p: ({ children }) => <p dir="auto" className="mb-2 last:mb-0">{children}</p>,
  ul: ({ children }) => <ul className="my-2 list-disc space-y-1 ps-5 last:mb-0">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 ps-5 last:mb-0">{children}</ol>,
  li: ({ children }) => <li dir="auto" className="ps-0.5">{children}</li>,
  h1: ({ children }) => <h2 dir="auto" className="mb-2 font-display text-lg font-semibold">{children}</h2>,
  h2: ({ children }) => <h3 dir="auto" className="mb-2 font-display text-base font-semibold">{children}</h3>,
  h3: ({ children }) => <h4 dir="auto" className="mb-1 font-semibold">{children}</h4>,
  blockquote: ({ children }) => (
    <blockquote dir="auto" className="my-2 border-s-2 border-primary/40 ps-3 text-ink-muted">{children}</blockquote>
  ),
  pre: ({ children }) => (
    <pre className="my-2 overflow-x-auto rounded-lg bg-canvas p-3 font-mono text-xs leading-relaxed last:mb-0">
      {children}
    </pre>
  ),
  code: ({ className, children }) => {
    if (className) return <code className={className}>{children}</code>
    return <code className="rounded bg-canvas/80 px-1 py-0.5 font-mono text-[0.9em]">{children}</code>
  },
  table: ({ children }) => (
    <div className="my-2 overflow-x-auto">
      <table className="w-full border-collapse text-sm">{children}</table>
    </div>
  ),
  th: ({ children }) => <th dir="auto" className="border-b border-line px-2 py-1 text-start font-medium">{children}</th>,
  td: ({ children }) => <td dir="auto" className="border-b border-line/60 px-2 py-1 align-top">{children}</td>,
}

// dir="auto" on each block: a paragraph in Arabic reads right to left even in
// an English UI, and English in an Arabic one left to right (spec §9).
//
// linkCheck marks the links that didn't open, or couldn't be checked, in
// the answer itself, not only in the notice under it (#209).
export function ChatMarkdown({ text, linkCheck }: { text: string; linkCheck?: LinkCheck }) {
  const { t } = useTranslation('chat')
  const components = useMemo<Components>(
    () => ({
      ...baseComponents,
      a: ({ href, children }) => {
        if (!href || !/^https?:\/\//i.test(href)) return <span>{children}</span>
        const status = statusOf(href, linkCheck)
        const label = status === 'dead' ? t('linkCheck.dead') : status === 'unknown' ? t('linkCheck.unknown') : ''
        return (
          <>
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className={`underline-offset-2 hover:underline ${status === 'dead' ? 'text-ink-muted line-through decoration-danger/60' : 'text-primary'}`}
            >
              {children}
            </a>
            {status ? (
              <span
                role="img"
                aria-label={label}
                title={label}
                data-link-status={status}
                className={`ms-0.5 inline-block align-super text-[0.7em] ${status === 'dead' ? 'text-danger' : 'text-ink-faint'}`}
              >
                {status === 'dead' ? '⚠' : '?'}
              </span>
            ) : null}
          </>
        )
      },
    }),
    [linkCheck, t],
  )
  return (
    <div className="chat-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {prepareChatMarkdown(text)}
      </ReactMarkdown>
    </div>
  )
}

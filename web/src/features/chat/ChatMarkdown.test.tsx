import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ChatMarkdown } from './ChatMarkdown'
import { prepareChatMarkdown } from './displayChatText'

const weather =
  'Here are some weather resources for Juneau, AK today: - [EaseWeather](https://www.easeweather.com/juneau/today): Hourly forecast. - [National Weather Service](https://forecast.weather.gov/MapClick.php?lat=58.3&lon=-134.4): Official forecast.'

describe('prepareChatMarkdown', () => {
  it('breaks an inline link list onto separate lines', () => {
    const prepared = prepareChatMarkdown(weather)
    expect(prepared).toContain('\n- [EaseWeather]')
    expect(prepared).toContain('\n- [National Weather Service]')
  })

  it('keeps a fenced code sample as code', () => {
    const sample = 'Example:\n```json\n{"query":"not a tool"}\n```'
    expect(prepareChatMarkdown(sample)).toContain('```json')
  })
})

describe('ChatMarkdown', () => {
  it('renders markdown links instead of the raw syntax', () => {
    render(<ChatMarkdown text={weather} />)
    const link = screen.getByRole('link', { name: 'EaseWeather' })
    expect(link).toHaveAttribute('href', 'https://www.easeweather.com/juneau/today')
    expect(screen.queryByText(/\]\(https/)).toBeNull()
  })

  it('does not turn a javascript URL into a link', () => {
    render(<ChatMarkdown text={'[click](javascript:alert(1))'} />)
    expect(screen.queryByRole('link')).toBeNull()
  })
})
// Links that didn't open, or couldn't be checked, are marked in the answer
// itself (#209); the rest look as before.
describe('ChatMarkdown link check', () => {
  const text = 'See [the guide](https://example.com/guide/), [the old page](https://example.com/old%20page), and [the account](https://example.com/account).'

  it('marks dead and unknown links, matching a trailing slash and encoding', () => {
    render(
      <ChatMarkdown
        text={text}
        linkCheck={{ dead: ['https://example.com/old page'], unknown: ['https://example.com/account/'] }}
      />,
    )
    expect(screen.getByRole('img', { name: "This link didn't open when checked" })).toHaveAttribute('data-link-status', 'dead')
    expect(screen.getByRole('img', { name: "This link couldn't be checked" })).toHaveAttribute('data-link-status', 'unknown')
    expect(screen.getAllByRole('img')).toHaveLength(2)
    expect(screen.getByRole('link', { name: 'the old page' })).toHaveClass('line-through')
    expect(screen.getByRole('link', { name: 'the guide' })).not.toHaveClass('line-through')
  })

  it('marks nothing without a check', () => {
    render(<ChatMarkdown text={text} />)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { OllamaPortCard } from './OllamaPort'

vi.mock('@/lib/api', () => ({
  api: {
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
  },
}))

const show = () =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <OllamaPortCard />
    </QueryClientProvider>,
  )

describe("OllamaPortCard (#1396)", () => {
  it('turns on answering on Ollama\'s port', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ api_port: 7331, ollama_port_enabled: false } as never)
    vi.mocked(api.updateSettings).mockResolvedValue({} as never)
    show()
    expect(await screen.findByText(/An app that lets you enter an address can use http:\/\/localhost:7331 instead/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('switch', { name: "Answer on Ollama's port" }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ ollama_port_enabled: true }))
  })

  it('says when it answers, and when another program has the port', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ ollama_port_enabled: true, ollama_port_listening: true } as never)
    const { unmount } = show()
    expect(await screen.findByText("Answering Ollama's API at http://localhost:11434.")).toBeInTheDocument()
    unmount()
    vi.mocked(api.getSettings).mockResolvedValue({ ollama_port_enabled: true, ollama_port_listening: false, ollama_port_error: 'in_use' } as never)
    show()
    expect(await screen.findByText(/probably Ollama, is using port 11434/)).toBeInTheDocument()
  })
})

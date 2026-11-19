import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { Connector } from '@/types/api'
import { ConnectedServices } from './ConnectedServices'

vi.mock('@/lib/api', () => ({
  api: {
    listConnectors: vi.fn(),
    connectService: vi.fn(),
    checkConnector: vi.fn(),
    disconnectService: vi.fn(),
  },
}))

const github: Connector = {
  id: 'github',
  name: 'GitHub',
  description: 'Search and read issues and pull requests, and comment on them.',
  scopes: 'Create a fine-grained personal access token limited to the repositories you want.',
  fields: [
    { key: 'token', label: 'Personal access token', secret: true },
    { key: 'api_url', label: 'API address', secret: false, optional: true },
  ],
  connected: false,
  tools: [
    { id: 'github.search', name: 'Search GitHub', description: '', risk: 'read', default_policy: 'allow' },
    { id: 'github.comment', name: 'Comment on GitHub', description: '', risk: 'write', default_policy: 'ask' },
  ],
}

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ConnectedServices />
    </QueryClientProvider>,
  )
}

describe('ConnectedServices', () => {
  beforeEach(() => {
    let current = github
    vi.mocked(api.listConnectors).mockReset().mockImplementation(async () => [current])
    vi.mocked(api.connectService).mockReset().mockImplementation(async (_id, values) => {
      if (values.token !== 'github_pat_good') throw new Error('could not connect to GitHub: the token was not accepted (401)')
      current = { ...github, connected: true, status: 'connected', account: '@octo', values: { token: '••••good' } }
      return current
    })
    vi.mocked(api.disconnectService).mockReset().mockImplementation(async () => {
      current = github
      return null
    })
  })

  it('connects with a token, shows the account, and never shows the token again', async () => {
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Connect GitHub' }))
    expect(screen.getByText(/fine-grained personal access token/)).toBeInTheDocument()
    const token = screen.getByLabelText('Personal access token')
    expect(token).toHaveAttribute('type', 'password')

    fireEvent.change(token, { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText(/the token was not accepted/)).toBeInTheDocument()

    fireEvent.change(token, { target: { value: 'github_pat_good' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText('Connected as @octo')).toBeInTheDocument()
    expect(screen.getByText(/Comment on GitHub \(asks first\)/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Change' }))
    const again = screen.getByLabelText('Personal access token')
    expect(again).toHaveValue('')
    expect(again).toHaveAttribute('placeholder', 'Stored (••••good). Leave blank to keep it.')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
    await waitFor(() => expect(screen.getByText('Not connected')).toBeInTheDocument())
  })

  // Mike's doorbell (#667): changing the Speaker alone failed with "Camera
  // address is required", and the example "front-door" read as a value.
  it('changes one setting without entering the others again, and shows examples as examples', async () => {
    const reolink: Connector = {
      id: 'reolink',
      name: 'Reolink',
      description: '',
      scopes: '',
      fields: [
        { key: 'url', label: 'Camera address', secret: false, placeholder: 'http://192.168.1.50' },
        { key: 'password', label: 'Password', secret: true },
        { key: 'talk_camera', label: 'Speaker', secret: false, optional: true, placeholder: 'front-door' },
        { key: 'talk_gateway', label: 'Gateway', secret: false, optional: true },
      ],
      connected: true,
      status: 'connected',
      values: { url: 'https://192.168.68.56', password: '••••word', talk_gateway: '127.0.0.1:9000' },
      tools: [],
    }
    vi.mocked(api.listConnectors).mockReset().mockResolvedValue([reolink])
    vi.mocked(api.connectService).mockReset().mockResolvedValue(reolink)
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Change' }))
    const speaker = screen.getByLabelText(/Speaker/)
    expect(speaker).toHaveValue('')
    expect(speaker).toHaveAttribute('placeholder', 'e.g. front-door')
    fireEvent.change(speaker, { target: { value: 'hub-ch0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.connectService).toHaveBeenCalledWith('reolink', {
        url: 'https://192.168.68.56',
        talk_gateway: '127.0.0.1:9000',
        talk_camera: 'hub-ch0',
      }),
    )
  })

  // Regression: toskar-core#1345
  // A camera's address that didn't answer is said at the address, in the
  // App language, and reolink-cli's settings are folded under Advanced.
  it('says an address error at the field and folds advanced settings away', async () => {
    const reolink: Connector = {
      id: 'reolink',
      name: 'Reolink',
      description: '',
      scopes: '',
      fields: [
        { key: 'url', label: 'Camera address', secret: false, placeholder: '192.168.1.50' },
        { key: 'password', label: 'Password', secret: true },
        { key: 'talk_camera', label: 'reolink-cli camera name', secret: false, optional: true, advanced: true },
      ],
      connected: false,
      tools: [],
    }
    vi.mocked(api.listConnectors).mockReset().mockResolvedValue([reolink])
    vi.mocked(api.connectService)
      .mockReset()
      .mockRejectedValue(
        Object.assign(new Error('could not reach'), {
          serviceMessage: 'could not reach',
          code: 'CAMERA_UNREACHABLE',
          details: { field: 'url' },
        }),
      )
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Connect Reolink' }))
    const address = screen.getByLabelText('Camera address')
    expect(address).toHaveAttribute('placeholder', 'e.g. 192.168.1.50')
    expect(screen.getByText('reolink-cli camera name').closest('details')).not.toBeNull()
    fireEvent.change(address, { target: { value: '192.168.1.99' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't reach a camera at that address/i)
    expect(address).toHaveAttribute('aria-invalid', 'true')
    expect(address).toHaveAttribute('aria-describedby', alert.id)
  })
})

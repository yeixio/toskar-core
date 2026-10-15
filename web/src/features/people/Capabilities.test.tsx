import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PersonCapabilities, RoleCapabilities } from './Capabilities'
import { attachAccept } from '@/lib/upload'

const all = ['files', 'make_video'] as const
const roles = {
  capabilities: [...all],
  roles: {
    owner: { files: true, make_video: true },
    admin: { files: true, make_video: true },
    member: { files: true, make_video: false },
    visitor: { files: false, make_video: false },
  },
  defaults: {
    owner: { files: true, make_video: true },
    admin: { files: true, make_video: true },
    member: { files: true, make_video: true },
    visitor: { files: false, make_video: false },
  },
}

vi.mock('@/lib/api', () => ({
  api: {
    getRoleCapabilities: vi.fn(() => Promise.resolve(roles)),
    setRoleCapabilities: vi.fn(() => Promise.resolve({})),
    getPersonCapabilities: vi.fn(() => Promise.resolve({ set: { make_video: true }, effective: { files: true, make_video: true } })),
    setPersonCapabilities: vi.fn(() => Promise.resolve({ set: {}, effective: {} })),
  },
}))

import { api } from '@/lib/api'

const wrap = (ui: React.ReactNode) => <QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>

describe('capabilities (#589)', () => {
  it("shows each role's, marks changes, and lets only the Owner change Admins'", async () => {
    render(wrap(<RoleCapabilities actor="admin" />))
    const memberVideo = await screen.findByLabelText('Making video for Member')
    expect(memberVideo).not.toBeChecked()
    expect(screen.getByLabelText('Making video for Admin')).toBeDisabled()
    expect(screen.getAllByTitle('Changed from the default')).toHaveLength(1)

    fireEvent.click(memberVideo)
    await waitFor(() => expect(api.setRoleCapabilities).toHaveBeenCalledWith('member', { files: true, make_video: true }))
  })

  it("sets one person's Allow, Block, or back to their role's", async () => {
    render(wrap(<PersonCapabilities person={{ id: 'p1', name: 'Mo', role: 'member', created_at: '', sign_in: true }} />))
    // Opening it loads their capabilities.
    const details = screen.getByText(/What they may use/).closest('details')!
    details.open = true
    fireEvent(details, new Event('toggle'))
    const video = await screen.findByLabelText('Making video for Mo')
    expect(video).toHaveValue('allow')
    fireEvent.change(video, { target: { value: 'role' } })
    await waitFor(() => expect(api.setPersonCapabilities).toHaveBeenCalledWith('p1', { make_video: null }))
  })

  it('offers only the files someone may attach', () => {
    expect(attachAccept({ files: false, pictures: true, voice: false })).toBe('.png,.jpg,.jpeg,.mp4,.mov,.m4v,.mkv')
    expect(attachAccept({ files: false, pictures: false, voice: false })).toBe('')
    expect(attachAccept({ files: true, pictures: false, voice: false })).toContain('.pdf')
  })
})

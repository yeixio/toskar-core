import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { anyoneCanConnect } from './networkAccess'
import { WhoConnects } from './WhoConnects'

describe('WhoConnects (#711)', () => {
  it('says people sign in with a password, devices with a code, and apps with a key', () => {
    const connect = vi.fn()
    render(
      <MemoryRouter>
        <WhoConnects access={{ people: 2, devices: 1, keys: 0, sign_in_service: false }} onConnectDevice={connect} />
      </MemoryRouter>,
    )
    expect(screen.getByText('Sign in with their username and password.')).toBeInTheDocument()
    expect(screen.getByText(/2 can sign in/)).toBeInTheDocument()
    expect(screen.getByText('Connect with a 6-digit code.')).toBeInTheDocument()
    expect(screen.getByText(/1 connected/)).toBeInTheDocument()
    expect(screen.getByText('Use an API key, created below.')).toBeInTheDocument()
    expect(screen.getByText('0 keys')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Connect a device' }))
    expect(connect).toHaveBeenCalled()
  })

  it('counts anyone who could connect, as the server does', () => {
    expect(anyoneCanConnect({ people: 0, devices: 0, keys: 0, sign_in_service: false })).toBe(false)
    expect(anyoneCanConnect({ people: 1, devices: 0, keys: 0, sign_in_service: false })).toBe(true)
    expect(anyoneCanConnect({ people: 0, devices: 0, keys: 0, sign_in_service: true })).toBe(true)
  })
})

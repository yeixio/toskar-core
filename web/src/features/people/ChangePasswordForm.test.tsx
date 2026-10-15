import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ChangePasswordForm } from './ChangePasswordForm'

vi.mock('@/lib/api', () => ({
  api: { changeMyPassword: vi.fn().mockResolvedValue({}), signOut: vi.fn().mockResolvedValue(undefined) },
}))

import { api } from '@/lib/api'

describe('ChangePasswordForm (#592)', () => {
  it('changes a password an Admin set, once the new one is typed twice', async () => {
    const onChanged = vi.fn()
    render(<ChangePasswordForm name="Ana" onChanged={onChanged} />)
    expect(screen.getByRole('heading')).toHaveTextContent('Choose your own password, Ana')
    const [given, next, again] = [
      screen.getByLabelText('Password you were given'),
      screen.getByLabelText('New password'),
      screen.getByLabelText('New password again'),
    ]
    fireEvent.change(given, { target: { value: 'given password 123' } })
    fireEvent.change(next, { target: { value: 'her own password' } })
    fireEvent.change(again, { target: { value: 'her own passwor' } })
    expect(screen.getByText("The new passwords don't match.")).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Change password' })).toBeDisabled()

    fireEvent.change(again, { target: { value: 'her own password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    expect(api.changeMyPassword).toHaveBeenCalledWith('given password 123', 'her own password')
  })
})

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ChatFolder } from './ChatFolder'

describe('ChatFolder', () => {
  it('sets a folder', async () => {
    const onChange = vi.fn().mockResolvedValue(undefined)
    render(<ChatFolder folder="" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'No folder' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: ' /work/app ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith('/work/app'))
    await waitFor(() => expect(screen.queryByRole('textbox')).toBeNull())
  })

  it('says why a folder was refused, and stays open', async () => {
    const onChange = vi.fn().mockRejectedValue(new Error("There's no folder at that path"))
    render(<ChatFolder folder="" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'No folder' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '/gone' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect((await screen.findByRole('alert')).textContent).toContain("There's no folder")
    expect(screen.getByRole('textbox')).toBeTruthy()
  })

  it('removes a folder, and names the one in use', async () => {
    const onChange = vi.fn().mockResolvedValue(undefined)
    render(<ChatFolder folder="/work/app" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Folder: app' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(''))
  })
})

describe('ChatFolder in a project (#1136)', () => {
  it("says the folder comes from the project when the chat has none of its own", () => {
    render(<ChatFolder folder="" projectFolder="/work/site" onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'From the project: site' }))
    expect(screen.getByText(/works in its project's folder, \/work\/site/)).toBeInTheDocument()
    // Nothing of the chat's own to remove.
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull()
  })

  it("names the chat's own folder over the project's", () => {
    render(<ChatFolder folder="/work/app" projectFolder="/work/site" onChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Folder: app' })).toBeInTheDocument()
  })
})

import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ContactTimeline } from '../components/ContactTimeline'
import type { ContactWindowsState } from '../hooks/useContactWindows'

const EMPTY_STATE: ContactWindowsState = {
  byGS: {},
  delivery: null,
  lastUpdated: Date.now(),
}

describe('ContactTimeline', () => {
  it('renders collapsed header by default', () => {
    render(<ContactTimeline windowsState={EMPTY_STATE} />)
    const btn = screen.getByRole('button', { name: /expand contact window timeline/i })
    expect(btn).toBeInTheDocument()
    expect(btn).toHaveAttribute('aria-expanded', 'false')
  })

  it('expands when header clicked', () => {
    render(<ContactTimeline windowsState={EMPTY_STATE} />)
    const btn = screen.getByRole('button', { name: /expand contact window timeline/i })
    fireEvent.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    // GS names should now be visible
    expect(screen.getByText('Nairobi')).toBeInTheDocument()
  })

  it('shows all 6 ground station rows when expanded', () => {
    render(<ContactTimeline windowsState={EMPTY_STATE} />)
    fireEvent.click(screen.getByRole('button'))
    const gsNames = ['Nairobi', 'Svalbard', 'Punta Arenas', 'Fairbanks', 'Bangalore', 'Perth']
    gsNames.forEach(name => {
      expect(screen.getByText(name)).toBeInTheDocument()
    })
  })

  it('shows IN PASS when delivery is flowing', () => {
    const state: ContactWindowsState = {
      ...EMPTY_STATE,
      delivery: { flowing: true, relay: 'NOAA-20', gs: 'Svalbard', aosMs: null, durationMs: 0 },
    }
    render(<ContactTimeline windowsState={state} />)
    expect(screen.getByText(/IN PASS/i)).toBeInTheDocument()
  })
})

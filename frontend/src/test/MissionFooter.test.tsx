import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { MissionFooter } from '../components/MissionFooter'

describe('MissionFooter', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-08T21:14:07Z'))
  })

  it('renders UTC clock', () => {
    render(<MissionFooter connected={true} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now() - 5000} />)
    expect(screen.getByLabelText(/UTC time/i)).toBeInTheDocument()
  })

  it('renders MET timer', () => {
    render(<MissionFooter connected={true} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now() - 5000} />)
    expect(screen.getByLabelText(/mission elapsed time/i)).toBeInTheDocument()
  })

  it('renders target satellite label', () => {
    render(<MissionFooter connected={true} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now()} />)
    expect(screen.getByLabelText(/active target/i)).toHaveTextContent('ISS')
  })

  it('renders GCS heartbeat dot', () => {
    render(<MissionFooter connected={true} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now()} />)
    expect(screen.getByLabelText(/ground control station link/i)).toBeInTheDocument()
  })

  it('shows connected state', () => {
    render(<MissionFooter connected={true} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now()} />)
    expect(screen.getByLabelText(/ground control station link: connected/i)).toBeInTheDocument()
  })

  it('shows offline state', () => {
    render(<MissionFooter connected={false} selectedSatId="ISS (ZARYA)" missionStartMs={Date.now()} />)
    expect(screen.getByLabelText(/ground control station link: offline/i)).toBeInTheDocument()
  })
})

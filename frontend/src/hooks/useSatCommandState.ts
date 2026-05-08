import { useEffect, useReducer } from 'react'

export type SatSystemMode = 'nominal' | 'safe' | 'rebooting'

export interface SatCommandStateMap {
  modes:           Record<string, SatSystemMode>          // satId → mode
  rebootPhase:     Record<string, 'red'|'hidden'|'done'>  // satId → reboot step
  deployedAgents:  Record<string, string[]>               // satId → agent list
  federatedBeam:   boolean
  executingSats:   Record<string, string>                 // satId → commandId
  orbitPredictors: Set<string>                            // satIds with orbit predictor
  anomalyDetectors: Set<string>                           // satIds with anomaly detector
  linkOptimizers:  Set<string>                            // satIds with link optimizer
  sonarTick:       number                                 // increments on each sonar pulse
  sonarSats:       string[]                               // satIds with active sonar ring
  blinkTick:       boolean                                // 200ms toggle for reboot blink
}

type Action =
  | { type: 'SAT_MODE';         satId: string; mode: SatSystemMode }
  | { type: 'REBOOT_PHASE';     satId: string; phase: 'red'|'hidden'|'done' }
  | { type: 'AGENT_DEPLOYED';   satId: string; agentId: string }
  | { type: 'FEDERATED_BEAM';   active: boolean }
  | { type: 'SAT_CMD';          satId: string; commandId: string; status: string }
  | { type: 'SONAR';            satId: string }
  | { type: 'SONAR_CLEAR';      satId: string }
  | { type: 'BLINK_TICK' }

const INITIAL: SatCommandStateMap = {
  modes:            {},
  rebootPhase:      {},
  deployedAgents:   {},
  federatedBeam:    false,
  executingSats:    {},
  orbitPredictors:  new Set(),
  anomalyDetectors: new Set(),
  linkOptimizers:   new Set(),
  sonarTick:        0,
  sonarSats:        [],
  blinkTick:        false,
}

function reducer(state: SatCommandStateMap, action: Action): SatCommandStateMap {
  switch (action.type) {
    case 'SAT_MODE':
      return { ...state, modes: { ...state.modes, [action.satId]: action.mode } }
    case 'REBOOT_PHASE': {
      const phase = { ...state.rebootPhase }
      if (action.phase === 'done') {
        delete phase[action.satId]
      } else {
        phase[action.satId] = action.phase
      }
      return { ...state, rebootPhase: phase }
    }
    case 'AGENT_DEPLOYED': {
      const existing = state.deployedAgents[action.satId] ?? []
      if (existing.includes(action.agentId)) return state
      const next = { ...state.deployedAgents, [action.satId]: [...existing, action.agentId] }
      const orbitPredictors = new Set(state.orbitPredictors)
      const anomalyDetectors = new Set(state.anomalyDetectors)
      const linkOptimizers = new Set(state.linkOptimizers)
      if (action.agentId === 'orbit-predictor')   orbitPredictors.add(action.satId)
      if (action.agentId === 'anomaly-detector')  anomalyDetectors.add(action.satId)
      if (action.agentId === 'link-optimizer')    linkOptimizers.add(action.satId)
      return { ...state, deployedAgents: next, orbitPredictors, anomalyDetectors, linkOptimizers }
    }
    case 'FEDERATED_BEAM':
      return { ...state, federatedBeam: action.active }
    case 'SAT_CMD': {
      const exec = { ...state.executingSats }
      if (action.status === 'executing') {
        exec[action.satId] = action.commandId
      } else {
        delete exec[action.satId]
      }
      return { ...state, executingSats: exec }
    }
    case 'SONAR':
      return { ...state, sonarSats: [...state.sonarSats.filter(s => s !== action.satId), action.satId], sonarTick: state.sonarTick + 1 }
    case 'SONAR_CLEAR':
      return { ...state, sonarSats: state.sonarSats.filter(s => s !== action.satId) }
    case 'BLINK_TICK':
      return { ...state, blinkTick: !state.blinkTick }
    default:
      return state
  }
}

export function useSatCommandState(): SatCommandStateMap {
  const [state, dispatch] = useReducer(reducer, INITIAL)

  useEffect(() => {
    const onMode = (e: Event) => {
      const { satId, mode } = (e as CustomEvent<{ satId: string; mode: SatSystemMode }>).detail
      dispatch({ type: 'SAT_MODE', satId, mode })
    }
    const onReboot = (e: Event) => {
      const { satId } = (e as CustomEvent<{ satId: string }>).detail
      dispatch({ type: 'SAT_MODE', satId, mode: 'rebooting' })
      dispatch({ type: 'REBOOT_PHASE', satId, phase: 'red' })
      setTimeout(() => dispatch({ type: 'REBOOT_PHASE', satId, phase: 'hidden' }), 500)
      setTimeout(() => {
        dispatch({ type: 'REBOOT_PHASE', satId, phase: 'done' })
        dispatch({ type: 'SAT_MODE', satId, mode: 'nominal' })
      }, 3500)
    }
    const onAgent = (e: Event) => {
      const { satId, agentId } = (e as CustomEvent<{ satId: string; agentId: string }>).detail
      dispatch({ type: 'AGENT_DEPLOYED', satId, agentId })
    }
    const onBeam = (e: Event) => {
      const { active } = (e as CustomEvent<{ active: boolean }>).detail
      dispatch({ type: 'FEDERATED_BEAM', active })
      if (active) setTimeout(() => dispatch({ type: 'FEDERATED_BEAM', active: false }), 8000)
    }
    const onCmd = (e: Event) => {
      const { satId, commandId, status } = (e as CustomEvent<{ satId: string; commandId: string; status: string }>).detail
      dispatch({ type: 'SAT_CMD', satId, commandId, status })
    }
    const onSonar = (e: Event) => {
      const { satId } = (e as CustomEvent<{ satId: string }>).detail
      dispatch({ type: 'SONAR', satId })
      setTimeout(() => dispatch({ type: 'SONAR_CLEAR', satId }), 2500)
    }

    window.addEventListener('sat-mode',       onMode)
    window.addEventListener('sat-reboot',     onReboot)
    window.addEventListener('agent-deployed', onAgent)
    window.addEventListener('federated-beam', onBeam)
    window.addEventListener('sat-cmd',        onCmd)
    window.addEventListener('sat-sonar',      onSonar)

    return () => {
      window.removeEventListener('sat-mode',       onMode)
      window.removeEventListener('sat-reboot',     onReboot)
      window.removeEventListener('agent-deployed', onAgent)
      window.removeEventListener('federated-beam', onBeam)
      window.removeEventListener('sat-cmd',        onCmd)
      window.removeEventListener('sat-sonar',      onSonar)
    }
  }, [])

  // 200ms blink tick for reboot red flash
  useEffect(() => {
    const hasRebooting = Object.keys(state.rebootPhase).some(k => state.rebootPhase[k] === 'red')
    if (!hasRebooting) return
    const id = setInterval(() => dispatch({ type: 'BLINK_TICK' }), 200)
    return () => clearInterval(id)
  }, [state.rebootPhase])

  return state
}

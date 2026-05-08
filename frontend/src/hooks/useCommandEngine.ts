import { useState, useCallback, createContext, useContext } from 'react'

export type CommandStatus = 'idle' | 'queued' | 'executing' | 'complete' | 'failed'

export interface TerminalEntry {
  ts:     number
  text:   string
  type:   'cmd' | 'boot' | 'ack' | 'error' | 'alarm' | 'edge' | 'sync' | 'ai'
  satId?: string
}

interface SatCommandState {
  status:    CommandStatus
  commandId: string
  progress:  number   // 0–100 for progress bar
}

export interface CommandEngineState {
  terminalLog:    TerminalEntry[]
  satState:       Record<string, SatCommandState>
  deployedAgents: Record<string, Set<string>>  // satId → set of deployed agent ids
  federatedBeam:  boolean
  systemMode:     Record<string, 'nominal' | 'safe' | 'rebooting'>
}

export interface CommandEngine {
  state:   CommandEngineState
  execute: (commandId: string, satId: string, params?: Record<string, string>) => void
  log:     (entry: Omit<TerminalEntry, 'ts'>) => void
}

const INITIAL_STATE: CommandEngineState = {
  terminalLog:    [],
  satState:       {},
  deployedAgents: {},
  federatedBeam:  false,
  systemMode:     {},
}

export const CommandEngineContext = createContext<CommandEngine | null>(null)

export function useCommandEngineContext(): CommandEngine {
  const ctx = useContext(CommandEngineContext)
  if (!ctx) throw new Error('CommandEngineContext not provided')
  return ctx
}

const MAX_LOG = 200

export function useCommandEngine(): CommandEngine {
  const [state, setState] = useState<CommandEngineState>(INITIAL_STATE)

  const log = useCallback((entry: Omit<TerminalEntry, 'ts'>) => {
    setState(s => {
      const entries = [...s.terminalLog, { ...entry, ts: Date.now() }]
      return { ...s, terminalLog: entries.length > MAX_LOG ? entries.slice(-MAX_LOG) : entries }
    })
  }, [])

  const setSatStatus = useCallback((satId: string, status: CommandStatus, commandId: string, progress = 0) => {
    setState(s => ({ ...s, satState: { ...s.satState, [satId]: { status, commandId, progress } } }))
  }, [])

  const execute = useCallback(async (commandId: string, satId: string, params?: Record<string, string>) => {
    const satLabel = satId.split(' ')[0].slice(0, 8)

    // Phase 1: synchronous flash (300ms) before real async engine
    setSatStatus(satId, 'executing', commandId, 0)

    // Dispatch sat-command-executing for Globe3D to pick up
    window.dispatchEvent(new CustomEvent('sat-cmd', { detail: { satId, commandId, status: 'executing' } }))

    await delay(300)

    // Route to simulation
    try {
      await simulateCommand(commandId, satId, satLabel, params ?? {}, log, setState)
      setSatStatus(satId, 'complete', commandId, 100)
      window.dispatchEvent(new CustomEvent('sat-cmd', { detail: { satId, commandId, status: 'complete' } }))
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'command failed'
      setSatStatus(satId, 'failed', commandId)
      log({ text: `[ERR] ${commandId} · ${satLabel} · ${msg}`, type: 'error', satId })
      window.dispatchEvent(new CustomEvent('sat-cmd', { detail: { satId, commandId, status: 'failed' } }))
    }

    // Return to idle after 2s
    await delay(2000)
    setSatStatus(satId, 'idle', commandId)
  }, [log, setSatStatus])

  return { state, execute, log }
}

function delay(ms: number) { return new Promise(r => setTimeout(r, ms)) }

// ─── Command Simulations ────────────────────────────────────────────────────

const SIMULATE = (import.meta as unknown as { env: Record<string, string> }).env.VITE_SIMULATE !== 'false'

async function simulateCommand(
  commandId: string,
  satId: string,
  satLabel: string,
  params: Record<string, string>,
  log: (e: Omit<TerminalEntry, 'ts'>) => void,
  setState: React.Dispatch<React.SetStateAction<CommandEngineState>>,
) {
  if (!SIMULATE) return  // real hardware path — no simulation

  switch (commandId) {
    case 'safe_mode':
      return simSafeMode(satId, satLabel, log)
    case 'nominal':
      return simNominal(satId, satLabel, log)
    case 'reboot_obc':
      return simReboot(satId, satLabel, log)
    case 'dump_tlm':
      return simDumpTelemetry(satId, satLabel, log)
    case 'health_scan':
      return simHealthScan(satId, satLabel, log)
    case 'set_pitch':
      return simSetPitch(satId, satLabel, params, log)
    case 'eclipse_forecast':
      return simEclipseForecast(satId, satLabel, log)
    case 'link_budget':
      return simLinkBudget(satId, satLabel, log)
    case 'deploy_anomaly':
      return simDeployAgent(satId, satLabel, 'anomaly-detector', log, setState)
    case 'deploy_compress':
      return simDeployAgent(satId, satLabel, 'telemetry-compressor', log, setState)
    case 'deploy_inference':
      return simDeployEdgeInference(satId, satLabel, log, setState)
    case 'deploy_orbit':
      return simDeployAgent(satId, satLabel, 'orbit-predictor', log, setState)
    case 'deploy_fedlearn':
      return simDeployFedLearner(satId, satLabel, log, setState)
    case 'deploy_linkopt':
      return simDeployAgent(satId, satLabel, 'link-optimizer', log, setState)
    default:
      log({ text: `[CMD] ${commandId} dispatched · ${satLabel}`, type: 'cmd', satId })
  }
}

async function simSafeMode(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[CMD] /safe-mode → ${satLabel}`, type: 'cmd', satId })
  await delay(400)
  log({ text: `[MODE] Subsystems powering down · ${satLabel}`, type: 'boot', satId })
  await delay(600)
  log({ text: `[MODE] State: CRITICAL · ${satLabel}`, type: 'boot', satId })
  // Signal alive dot to amber
  window.dispatchEvent(new CustomEvent('sat-mode', { detail: { satId, mode: 'safe' } }))
}

async function simNominal(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[CMD] /nominal → ${satLabel}`, type: 'cmd', satId })
  await delay(400)
  log({ text: `[MODE] Telemetry streams restored · ${satLabel}`, type: 'ack', satId })
  window.dispatchEvent(new CustomEvent('sat-mode', { detail: { satId, mode: 'nominal' } }))
}

async function simReboot(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[CMD] /reboot-obc → ${satLabel}`, type: 'cmd', satId })
  await delay(300)
  // Signal globe to blink red + disappear
  window.dispatchEvent(new CustomEvent('sat-reboot', { detail: { satId } }))
  log({ text: `[BOOT] Initializing Kernel... · ${satLabel}`, type: 'boot', satId })
  await delay(1200)
  log({ text: `[BOOT] Loading Flight Software v4.2... · ${satLabel}`, type: 'boot', satId })
  await delay(1200)
  log({ text: `[BOOT] Link Re-established · ${satLabel}`, type: 'ack', satId })
}

async function simDumpTelemetry(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[TLM] Initiating frame dump · ${satLabel}`, type: 'cmd', satId })
  const hexFrames = ['0x4A 0x22 0xFF 0x01 0x7C', '0xB3 0x88 0x00 0x44', '0xA1 0xF3 0x12 0x00 0x09']
  for (let i = 0; i < 8; i++) {
    await delay(150)
    const frame = hexFrames[i % hexFrames.length]
    log({ text: `[TLM] ${frame} ... seq=${1000 + i}`, type: 'cmd', satId })
  }
  log({ text: `[TLM] Dump complete · 8 frames · ${satLabel}`, type: 'ack', satId })
  window.dispatchEvent(new CustomEvent('dump-progress', { detail: { satId, pct: 100 } }))
}

async function simHealthScan(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[SCAN] Health scan started · ${satLabel}`, type: 'cmd', satId })
  const systems = ['EPS', 'TTC', 'ADCS', 'OBDH', 'TCS']
  for (const sys of systems) {
    await delay(300)
    log({ text: `[SCAN] ${sys}: OK · ${satLabel}`, type: 'ack', satId })
  }
  log({ text: `[SCAN] All subsystems nominal · ${satLabel}`, type: 'ack', satId })
  // Sonar ring on globe
  window.dispatchEvent(new CustomEvent('sat-sonar', { detail: { satId } }))
}

async function simSetPitch(
  satId: string, satLabel: string,
  params: Record<string, string>,
  log: (e: Omit<TerminalEntry, 'ts'>) => void,
) {
  const val = params.value ?? '1.5'
  log({ text: `[ADCS] Pitch threshold → ±${val}° · ${satLabel}`, type: 'ack', satId })
}

async function simEclipseForecast(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[FCST] Computing eclipse windows · ${satLabel}`, type: 'cmd', satId })
  await delay(500)
  log({ text: `[FCST] Next eclipse entry: +23m 14s · ${satLabel}`, type: 'ack', satId })
  log({ text: `[FCST] Duration: ~36 min · ${satLabel}`, type: 'ack', satId })
}

async function simLinkBudget(satId: string, satLabel: string, log: (e: Omit<TerminalEntry, 'ts'>) => void) {
  log({ text: `[LINK] Computing link budget · ${satLabel}`, type: 'cmd', satId })
  await delay(400)
  log({ text: `[LINK] Nairobi: Eb/N0=12.4 dB MARGIN=+2.4 dB · ${satLabel}`, type: 'ack', satId })
  log({ text: `[LINK] Svalbard: Eb/N0=14.1 dB MARGIN=+4.1 dB · ${satLabel}`, type: 'ack', satId })
}

async function simDeployAgent(
  satId: string, satLabel: string, agentId: string,
  log: (e: Omit<TerminalEntry, 'ts'>) => void,
  setState: React.Dispatch<React.SetStateAction<CommandEngineState>>,
) {
  log({ text: `[DEPLOY] Uploading ${agentId} → ${satLabel}`, type: 'cmd', satId })
  for (let i = 0; i <= 100; i += 20) {
    await delay(200)
    window.dispatchEvent(new CustomEvent('deploy-progress', { detail: { satId, agentId, pct: i } }))
  }
  log({ text: `[DEPLOY] ${agentId} active · ${satLabel}`, type: 'ack', satId })
  setState(s => {
    const agents = new Set(s.deployedAgents[satId] ?? [])
    agents.add(agentId)
    return { ...s, deployedAgents: { ...s.deployedAgents, [satId]: agents } }
  })
  window.dispatchEvent(new CustomEvent('agent-deployed', { detail: { satId, agentId } }))
}

async function simDeployEdgeInference(
  satId: string, satLabel: string,
  log: (e: Omit<TerminalEntry, 'ts'>) => void,
  setState: React.Dispatch<React.SetStateAction<CommandEngineState>>,
) {
  await simDeployAgent(satId, satLabel, 'edge-inference', log, setState)
  await delay(200)
  log({ text: `[EDGE] Model loaded · 7-class anomaly detector · 15ms inference · ${satLabel}`, type: 'edge', satId })
  // Badge on globe icon
  window.dispatchEvent(new CustomEvent('agent-deployed', { detail: { satId, agentId: 'edge-inference', badge: '⋆ EDGE INFERENCE' } }))
}

async function simDeployFedLearner(
  satId: string, satLabel: string,
  log: (e: Omit<TerminalEntry, 'ts'>) => void,
  setState: React.Dispatch<React.SetStateAction<CommandEngineState>>,
) {
  await simDeployAgent(satId, satLabel, 'federated-learner', log, setState)
  await delay(200)
  log({ text: `[SYNC] Exchanging local model weights with peer assets...`, type: 'sync', satId })
  // Trigger 3-way federated beam on globe
  setState(s => ({ ...s, federatedBeam: true }))
  window.dispatchEvent(new CustomEvent('federated-beam', { detail: { active: true } }))
  await delay(3000)
  log({ text: `[SYNC] Federated gradient exchange complete`, type: 'ack', satId })
}

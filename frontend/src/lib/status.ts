// Derives the per-lesson pipeline state shown in the step bar and lesson list.
// Each step: { key, state, reason } with state one of
//   done | action (user can/should act) | running | blocked | todo
// `reason` is an i18n key explaining a blocked/action state.
import { STEPS, type Step } from './router.ts'
import type { Lesson, Rec, Summary } from '../api/types.ts'

export const ACTIVE = ['queued', 'running', 'paused']

export type StepState = 'done' | 'action' | 'running' | 'blocked' | 'todo'
export interface StepStatus {
  key: Step
  state: StepState
  reason?: string
}

export function lessonSteps(lesson: Rec<Lesson> | undefined, snapshot: Summary): StepStatus[] {
  if (!lesson) return []
  const id = lesson.id
  const sources = (snapshot.source || []).filter(s => s.lesson_id === id)
  const research = sources.filter(s => s.data.role === 'research')
  const tasks = (snapshot.task || []).filter(t => t.lesson_id === id)
  const parsing = tasks.some(t => t.data.type === 'parse' && ACTIVE.includes(t.data.status))
  const knowledge = (snapshot.knowledge || []).find(k => k.id === lesson.data.knowledge_id)
  const generating = tasks.some(t => t.data.type !== 'parse' && ACTIVE.includes(t.data.status))
  const artifacts = (snapshot.artifact || []).filter(a => a.lesson_id === id || a.data.grouped_lesson_ids?.includes(id))

  const s = {} as Record<Step, Omit<StepStatus, 'key'>>
  if (parsing) s.sources = { state: 'running', reason: 'stepParsing' }
  else if (knowledge) s.sources = { state: 'done' }
  else if (research.length) s.sources = { state: 'action', reason: 'stepNeedsParse' }
  else s.sources = { state: 'action', reason: 'stepNeedsSource' }

  if (!knowledge) s.knowledge = { state: 'blocked', reason: 'stepNeedsParse' }
  else if (knowledge.data.confirmed) s.knowledge = { state: 'done' }
  else s.knowledge = { state: 'action', reason: 'stepNeedsConfirm' }

  if (!knowledge?.data.confirmed) s.outputs = { state: 'blocked', reason: 'confirm_knowledge_first' }
  else if (generating) s.outputs = { state: 'running', reason: 'stepGenerating' }
  else if (artifacts.length) s.outputs = { state: 'done' }
  else s.outputs = { state: 'todo' }


  return STEPS.map(key => ({ key, ...s[key] }))
}

// The step a user should look at next: first non-done, non-blocked step.
export function nextStep(steps: StepStatus[]): Step {
  return (steps.find(step => step.state === 'action' || step.state === 'running') || steps.find(step => step.state === 'todo') || steps[0])?.key || 'sources'
}

export function activeTasks(snapshot: Summary) {
  return (snapshot.task || []).filter(t => ACTIVE.includes(t.data.status))
}

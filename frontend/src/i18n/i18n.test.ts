import { describe, expect, it } from 'vitest'
import { messages } from './index'

function leafKeys(value: object, prefix = ''): string[] {
  return Object.entries(value).flatMap(([key, child]: [string, unknown]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof child === 'object' && child !== null) return leafKeys(child, path)
    if (typeof child !== 'string' || child.trim() === '') throw new Error(`Invalid translation: ${path}`)
    return [path]
  }).sort()
}

describe('interface translations', () => {
  it('provides the same non-empty messages for both languages', () => {
    expect(leafKeys(messages['zh-TW'])).toEqual(leafKeys(messages.en))
  })
})


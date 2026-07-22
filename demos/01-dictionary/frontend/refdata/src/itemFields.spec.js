import { describe, expect, it } from 'vitest'

import { attrsFor, codeFor, labelFor, statusFor } from './itemFields'

describe('itemFields', () => {
  describe('flat item shape (no locale requested)', () => {
    const item = { code: 'at-anchor', status: 'deprecated', attrs: { name: 'At Anchor' } }

    it('codeFor reads the top-level code', () => {
      expect(codeFor(item)).toBe('at-anchor')
    })
    it('statusFor reads the top-level status', () => {
      expect(statusFor(item)).toBe('deprecated')
    })
    it('attrsFor reads the top-level attrs', () => {
      expect(attrsFor(item)).toEqual({ name: 'At Anchor' })
    })
    it('labelFor prefers attrs.name over the code', () => {
      expect(labelFor(item)).toBe('At Anchor')
    })
  })

  describe('locale-resolved item shape ({ item, label })', () => {
    const resolved = {
      label: 'Voor Anker',
      item: { code: 'at-anchor', status: 'active', attrs: { name: 'At Anchor' } },
    }

    it('codeFor reads through item.item.code', () => {
      expect(codeFor(resolved)).toBe('at-anchor')
    })
    it('statusFor reads through item.item.status', () => {
      expect(statusFor(resolved)).toBe('active')
    })
    it('attrsFor reads through item.item.attrs', () => {
      expect(attrsFor(resolved)).toEqual({ name: 'At Anchor' })
    })
    it('labelFor prefers the resolved label over attrs.name', () => {
      expect(labelFor(resolved)).toBe('Voor Anker')
    })
  })

  describe('defaults', () => {
    it('statusFor defaults to active when no status is present', () => {
      expect(statusFor({ code: 'x' })).toBe('active')
    })
    it('labelFor falls back to the code when no label or attrs.name exists', () => {
      expect(labelFor({ code: 'x', attrs: {} })).toBe('x')
    })
    it('attrsFor defaults to an empty object', () => {
      expect(attrsFor({ code: 'x' })).toEqual({})
    })
  })
})

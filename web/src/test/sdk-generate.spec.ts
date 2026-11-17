import { describe, it, expect } from 'vitest'
// @ts-expect-error generator is an executable JavaScript module
import { generate } from '../../../sdk/generate.mjs'
describe('SDK type generator', () => {
  it('uses published definitions, required fields, enums and safe Go field names', () => {
    const points = [{ id: 10, code: 'purchase', definition_state: 'PUBLISHED', properties: [
      { code: 'order_id', type: 'STRING', required: true },
      { code: 'state', type: 'ENUM', enum_values: ['paid', 'failed'], required: false },
      { code: 'order__id', type: 'INTEGER', required: false },
    ] }, { id: 11, code: 'draft', definition_state: 'DRAFT', properties: [] }]
    const ts = generate(points, 'ts'); expect(ts).toContain('"purchase"'); expect(ts).toContain('"state"?: "paid" | "failed"'); expect(ts).not.toContain('"draft"')
    const go = generate(points, 'go'); expect(go).toContain('OrderId string `json:"order_id"`'); expect(go).toContain('OrderIdField2 *int64 `json:"order__id,omitempty"`')
  })
})

import { describe, expect, it, vi } from 'vitest'
import { Client, uuidv7 } from '../../../sdk/web/index.js'

describe('web SDK', () => {
  it('retries with the same IDs and exposes rejected results', async () => {
    const bodies: string[] = []
    const fetchImpl = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      bodies.push(init?.body as string)
      if (bodies.length === 1) return new Response('', { status: 503 })
      const { events } = JSON.parse(bodies[1]!)
      return new Response(JSON.stringify({ accepted: 1, rejected: 1, results: events.map((e: {event_id:string}, index:number) => ({ index, event_id:e.event_id, status:index ? 'rejected' : 'accepted', code:index ? 'PROPERTY_REQUIRED' : undefined })) }), {status:202})
    })
    const client = new Client({baseURL:'https://example.test', writeKey:'key', maxRetries:1,retryBackoff:1,fetchImpl})
    client.track({point_code:'view',platform:'web',anonymous_id:'a'})
    client.track({point_code:'click',platform:'web',anonymous_id:'a'})
    const results = await client.flush()
    expect(bodies[0]).toBe(bodies[1]); expect(results[1]?.code).toBe('PROPERTY_REQUIRED'); expect(client.pending()).toHaveLength(0)
    expect(uuidv7()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  })
  it('retains a failed or malformed batch for explicit retry', async () => {
    const client = new Client({baseURL:'https://example.test',writeKey:'key',maxRetries:0,fetchImpl:vi.fn(async()=>new Response('{"accepted":1,"rejected":0,"results":[]}',{status:202}))})
    const id=client.track({point_code:'view',platform:'web',anonymous_id:'a'})
    await expect(client.flush()).rejects.toThrow('malformed response')
    expect(client.pending()[0]?.event_id).toBe(id)
  })
})

describe('web SDK batch recovery', () => {
  it('returns completed batches on failure and retains only unsent events', async () => {
    let calls=0
    const client=new Client({baseURL:'https://example.test',writeKey:'key',maxRetries:0,fetchImpl:vi.fn(async (_url:RequestInfo|URL,init?:RequestInit)=>{
      calls++; if(calls===2) return new Response('',{status:503})
      const {events}=JSON.parse(init?.body as string)
      return new Response(JSON.stringify({accepted:events.length,rejected:0,results:events.map((e:{event_id:string},index:number)=>({index,event_id:e.event_id,status:'accepted'}))}),{status:202})
    })})
    for(let i=0;i<101;i++) client.track({point_code:'view',platform:'web',anonymous_id:'a'})
    try { await client.flush(); throw new Error('expected failure') } catch (error) {
      expect((error as Error & {completed: unknown[]}).completed).toHaveLength(100)
      expect(client.pending()).toHaveLength(1)
    }
  })
})

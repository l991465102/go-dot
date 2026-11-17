#!/usr/bin/env node
// 从管理 API 的已发布点位生成 TypeScript 或 Go 属性类型。
import { pathToFileURL } from 'node:url'

const tsType = p => ({ STRING:'string', ENUM:p.enum_values?.length ? p.enum_values.map(v => JSON.stringify(v)).join(' | ') : 'string', INTEGER:'number', DECIMAL:'number', BOOLEAN:'boolean', DATETIME:'string', ARRAY:`${({STRING:'string',INTEGER:'number',DECIMAL:'number',BOOLEAN:'boolean',DATETIME:'string'})[p.array_item_type] || 'unknown'}[]` })[p.type] || 'unknown'
const goType = p => ({ STRING:'string', ENUM:'string', INTEGER:'int64', DECIMAL:'float64', BOOLEAN:'bool', DATETIME:'string', ARRAY:`[]${({STRING:'string',INTEGER:'int64',DECIMAL:'float64',BOOLEAN:'bool',DATETIME:'string'})[p.array_item_type] || 'any'}` })[p.type] || 'any'
const identifier = code => code.split('_').filter(Boolean).map(part => part[0].toUpperCase()+part.slice(1)).join('') || 'Field'
export function generate(points, format) {
  if (!['ts','go'].includes(format)) throw new Error('format must be ts or go')
  const filtered=points.filter(p => p.definition_state === 'PUBLISHED').sort((a,b)=>a.id-b.id)
  if (format==='ts') {
    const lines=['// Generated from published go-dot definitions; do not edit.','export interface TrackingEvents {']
    for(const p of filtered){ lines.push(`  ${JSON.stringify(p.code)}: {`);for(const prop of p.properties || []) lines.push(`    ${JSON.stringify(prop.code)}${prop.required?'':'?'}: ${tsType(prop)};`);lines.push('  };') }
    return [...lines,'}','export type PointCode = keyof TrackingEvents;',''].join('\n')
  }
  const lines=['// Code generated from published go-dot definitions; DO NOT EDIT.','package trackingtypes','']
  for(const p of filtered){lines.push(`const Point${p.id} = ${JSON.stringify(p.code)}`,`type Properties${p.id} struct {`);const used=new Set();for(const prop of p.properties||[]){const base=identifier(prop.code);let name=base, suffix=2;while(used.has(name))name=`${base}Field${suffix++}`;used.add(name);const typ=goType(prop);lines.push(`\t${name} ${prop.required?'':'*'}${typ} \`json:${JSON.stringify(prop.code+(prop.required?'':',omitempty'))}\``)}lines.push('}','')}
  return lines.join('\n')
}
async function main(){
 const [base,project,format]=process.argv.slice(2),token=process.env.GO_DOT_ACCESS_TOKEN
 if(!base||!/^\d+$/.test(project||'')||!['ts','go'].includes(format)||!token)throw new Error('usage: GO_DOT_ACCESS_TOKEN=... node sdk/generate.mjs BASE_URL PROJECT_ID ts|go')
 const url=new URL(base);if(!['http:','https:'].includes(url.protocol)||url.search||url.hash)throw new Error('invalid base URL')
 const points=[];for(let page=1;;page++){const u=new URL(url);u.pathname=u.pathname.replace(/\/$/,'')+`/api/v1/projects/${project}/points`;u.searchParams.set('definition_state','PUBLISHED');u.searchParams.set('page',String(page));u.searchParams.set('page_size','100');const res=await fetch(u,{headers:{Authorization:`Bearer ${token}`},redirect:'error'});if(!res.ok)throw new Error(`point API returned HTTP ${res.status}`);const data=await res.json();if(!Array.isArray(data.items)||!Number.isSafeInteger(data.total))throw new Error('invalid point API response');points.push(...data.items);if(points.length>=data.total)break;if(!data.items.length)throw new Error('incomplete point listing')}
 process.stdout.write(generate(points,format))
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href)main().catch(err=>{console.error(err.message);process.exitCode=1})

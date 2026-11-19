/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { SessionIdentity, SessionIdentitySource } from '../types'

export const SESSION_IDENTITY_SOURCE_TYPES = [
  'metadata_user_id',
  'request_header',
  'gjson',
  'regex',
] as const

export const SESSION_IDENTITY_TRANSFORMS = [
  { value: '', labelKey: 'None (use raw value)' },
  { value: 'xxhash64', labelKey: 'xxHash64 (16 hex chars)' },
  { value: 'sha1_16', labelKey: 'SHA-1 (first 16 hex chars)' },
] as const

export const SESSION_IDENTITY_ON_EMPTY_POLICIES = [
  { value: 'legacy', labelKey: 'Legacy (first message, then fingerprint)' },
  { value: 'stable', labelKey: 'Stable (fingerprint only)' },
  { value: 'none', labelKey: 'None (do not synthesize)' },
] as const

export function createDefaultSessionIdentity(): SessionIdentity {
  return {
    enabled: false,
    model_prefixes: ['claude-'],
    sources: [],
    transform: 'xxhash64',
    value_regex: '',
    on_empty: 'legacy',
    disable_first_message_fallback: false,
    ttl_seconds: 0,
  }
}

export function createDefaultSessionIdentitySource(): SessionIdentitySource {
  return {
    type: 'regex',
    key: '',
    path: 'system.0.text',
    json_string_field: '',
    pattern: '',
    capture_group: 1,
  }
}

/**
 * Extracts top-level JSON objects from pasted text. Handles both minified
 * JSONL (one object per line) and concatenated/pretty-printed objects, while
 * ignoring braces inside JSON strings.
 */
export function splitJsonBodies(text: string): string[] {
  const bodies: string[] = []
  let depth = 0
  let start = -1
  let inString = false
  let escaped = false

  for (let i = 0; i < text.length; i++) {
    const char = text[i]
    if (inString) {
      if (escaped) {
        escaped = false
      } else if (char === '\\') {
        escaped = true
      } else if (char === '"') {
        inString = false
      }
      continue
    }
    if (char === '"') {
      inString = true
    } else if (char === '{') {
      if (depth === 0) start = i
      depth++
    } else if (char === '}') {
      if (depth > 0) {
        depth--
        if (depth === 0 && start >= 0) {
          bodies.push(text.slice(start, i + 1))
          start = -1
        }
      }
    }
  }

  return bodies
}

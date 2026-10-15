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

// A single override template row as edited in the UI. `content` is kept as a
// JSON string in the editor (parameter override object for param templates, or
// a JSON array of retry rules for retry templates). The synthetic `id` is a
// UI-only field used for table row keys and is stripped before saving.
export interface OverrideTemplateItem {
  id?: number
  name: string
  description?: string
  content: string
}

// Option-key map for the two root-only override template options. Both values
// are JSON STRINGs (an array of `{ name, description?, content }` once parsed).
export interface OverrideTemplatesSettings {
  'override_template_setting.param_templates': string
  'override_template_setting.retry_templates': string
}

export type OverrideTemplateKind = 'param' | 'retry'

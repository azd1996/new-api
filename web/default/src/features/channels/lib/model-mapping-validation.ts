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
// ============================================================================
// Model Mapping Validation Utilities
// ============================================================================

/**
 * Parse models string to array
 */
export function parseModelsString(modelsStr: string): string[] {
  return modelsStr
    ? modelsStr
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean)
    : []
}

/**
 * Format models array to string
 */
export function formatModelsArray(models: string[]): string {
  return [...new Set(models)].join(',')
}

/**
 * Normalize model name
 */
export function normalizeModelName(model: string): string {
  return typeof model === 'string' ? model.trim() : ''
}

/**
 * Extract source keys from model_mapping JSON
 * (the keys of the mapping object — models being remapped FROM)
 */
export function extractMappingSourceModels(modelMapping: string): string[] {
  if (typeof modelMapping !== 'string') return []
  const trimmed = modelMapping.trim()
  if (!trimmed) return []

  try {
    const parsed = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return []
    }

    const keys = Object.keys(parsed)
      .map((key) => key.trim())
      .filter(Boolean)

    return [...new Set(keys)]
  } catch {
    return []
  }
}

/**
 * Extract redirect models from model_mapping JSON
 */
export function extractRedirectModels(modelMapping: string): string[] {
  const mapping = modelMapping
  if (typeof mapping !== 'string') return []
  const trimmed = mapping.trim()
  if (!trimmed) return []

  try {
    const parsed = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return []
    }

    const values = Object.values(parsed)
      .map((value) => (typeof value === 'string' ? value.trim() : undefined))
      .filter((value): value is string => Boolean(value))

    return [...new Set(values)]
  } catch {
    return []
  }
}

/**
 * Check if model configuration has changed
 */
export function hasModelConfigChanged(
  currentModels: string[],
  currentModelMapping: string,
  initialModels: string[],
  initialModelMapping: string
): boolean {
  // Always return true if not editing (new channel)
  if (initialModels.length === 0 && !initialModelMapping) {
    return true
  }

  // Check if models array changed
  if (currentModels.length !== initialModels.length) {
    return true
  }
  for (let i = 0; i < currentModels.length; i++) {
    if (currentModels[i] !== initialModels[i]) {
      return true
    }
  }

  // Check if model_mapping changed
  const normalizedCurrent = (currentModelMapping || '').trim()
  const normalizedInitial = (initialModelMapping || '').trim()

  return normalizedCurrent !== normalizedInitial
}

/**
 * Find models in model_mapping that are missing from the models list
 */
export function findMissingModelsInMapping(
  modelMapping: string,
  currentModels: string[]
): string[] {
  if (!modelMapping || modelMapping.trim() === '') {
    return []
  }

  let parsedMapping: Record<string, unknown>
  try {
    parsedMapping = JSON.parse(modelMapping)
    if (
      !parsedMapping ||
      typeof parsedMapping !== 'object' ||
      Array.isArray(parsedMapping)
    ) {
      return []
    }
  } catch {
    return []
  }

  const modelSet = new Set(currentModels.map((m) => normalizeModelName(m)))
  const missingModels = Object.keys(parsedMapping)
    .map((key) => normalizeModelName(key))
    .filter((key) => key && !modelSet.has(key))

  return [...new Set(missingModels)]
}

/**
 * Validate model mapping JSON format
 */
export function validateModelMappingJson(modelMapping: string): {
  valid: boolean
  error?: string
} {
  if (!modelMapping || modelMapping.trim() === '') {
    return { valid: true }
  }

  try {
    const parsed = JSON.parse(modelMapping)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {
        valid: false,
        error: 'Model mapping must be a valid JSON object',
      }
    }
    if (Object.values(parsed).some((value) => typeof value !== 'string')) {
      return {
        valid: false,
        error: 'Model mapping values must be strings',
      }
    }
    return { valid: true }
  } catch {
    return {
      valid: false,
      error: 'Model mapping must be valid JSON format',
    }
  }
}

/**
 * Get redirect models that are also in the models list
 * (These should be removed from models list to keep /v1/models clean)
 */
export function findExposedTargetModels(
  modelMapping: string,
  currentModels: string[]
): string[] {
  const redirectModels = extractRedirectModels(modelMapping)
  if (redirectModels.length === 0) return []

  const normalizedModels = currentModels.map((m) => normalizeModelName(m))
  const modelSet = new Set(normalizedModels)

  return redirectModels.filter((model) =>
    modelSet.has(normalizeModelName(model))
  )
}

/**
 * Determine whether a model name is a generated customer alias.
 *
 * A model `m` is a generated customer alias iff there exists a customer `c`
 * (after trimming) such that `m` ends with `"_" + c` AND the prefix
 * `m[:len(m) - len("_" + c)]` is itself present in `baseModelSet`. This mirrors
 * the backend alias-expansion rule (`<baseModel>_<customer>` with a single
 * underscore delimiter). Empty customers are ignored.
 */
export function isCustomerAliasModel(
  model: string,
  customers: string[],
  baseModelSet: Set<string>
): boolean {
  const normalizedModel = normalizeModelName(model)
  if (!normalizedModel) return false

  for (const rawCustomer of customers) {
    const customer = normalizeModelName(rawCustomer)
    if (!customer) continue

    const suffix = `_${customer}`
    if (!normalizedModel.endsWith(suffix)) continue

    const prefix = normalizedModel.slice(0, normalizedModel.length - suffix.length)
    if (prefix.length === 0) continue
    if (baseModelSet.has(prefix)) return true
  }

  return false
}

/**
 * Strip generated customer alias models from a model list, returning the base
 * models only. Alias detection uses the full `models` array as the membership
 * set so that `<baseModel>_<customer>` aliases whose base is present are
 * removed. Empty customers are ignored; inputs are trimmed.
 */
export function stripCustomerAliasModels(
  models: string[],
  customers: string[]
): string[] {
  const normalizedModels = models
    .map((model) => normalizeModelName(model))
    .filter(Boolean)
  const baseModelSet = new Set(normalizedModels)

  return normalizedModels.filter(
    (model) => !isCustomerAliasModel(model, customers, baseModelSet)
  )
}

/**
 * Strip generated customer alias entries from a model_mapping object, returning
 * the manual-only mappings. A mapping entry `key -> value` is considered a
 * generated alias entry iff `key` is a detected customer alias relative to
 * `modelSet`. Empty customers are ignored; keys are trimmed.
 */
export function stripCustomerAliasMappings(
  mapping: Record<string, string>,
  modelSet: Set<string>,
  customers: string[]
): Record<string, string> {
  const result: Record<string, string> = {}

  for (const [rawKey, value] of Object.entries(mapping)) {
    const key = normalizeModelName(rawKey)
    if (!key) continue
    if (isCustomerAliasModel(key, customers, modelSet)) continue
    result[rawKey] = value
  }

  return result
}

/**
 * Categorize models into different sets for UI display
 */
export function categorizeModelsWithRedirect(
  currentModels: string[],
  redirectModels: string[]
): {
  normalizedCurrentModels: Set<string>
  normalizedRedirectModels: Set<string>
  classificationSet: Set<string>
  redirectOnlySet: Set<string>
} {
  const normalizedCurrentModels = new Set(
    currentModels.map((m) => normalizeModelName(m)).filter(Boolean)
  )

  const normalizedRedirectModels = new Set(
    redirectModels.map((m) => normalizeModelName(m)).filter(Boolean)
  )

  const classificationSet = new Set([
    ...normalizedCurrentModels,
    ...normalizedRedirectModels,
  ])

  const redirectOnlySet = new Set(
    [...normalizedRedirectModels].filter(
      (m) => !normalizedCurrentModels.has(m)
    )
  )

  return {
    normalizedCurrentModels,
    normalizedRedirectModels,
    classificationSet,
    redirectOnlySet,
  }
}

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { FileCode2Icon, RefreshCwIcon, Settings2Icon, TriangleAlertIcon } from '@lucide/vue'
import { parse, parseDocument, stringify } from 'yaml'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldTitle } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { getCPAConfig, updateCPAConfig } from '@/features/settings/api/settingsApi'
import { useI18n } from '@/shared/i18n'
import { cpaConfigFields, cpaConfigSections } from './cpaConfigFields'
import type { CPAConfigFieldDefinition } from './cpaConfigFields'
import { cpaConfigPath } from './cpaConfigPaths'

type ConfigValue = string | boolean

const { errorText, t } = useI18n()
const isLoading = ref(false)
const isLoaded = ref(false)
const loadError = ref('')
const activeSection = ref('server')
const editorMode = ref<'visual' | 'source'>('visual')
const sourceContent = ref('')
const sourceBaseline = ref('')
const apiVersion = ref(0)
const revision = ref('')
const values = reactive<Record<string, ConfigValue>>({})
const valueBaseline = ref<Record<string, ConfigValue>>({})

const visualDirty = computed(() => cpaConfigFields.some(field => values[field.key] !== valueBaseline.value[field.key]))
const sourceDirty = computed(() => sourceContent.value !== sourceBaseline.value)
const isDirty = computed(() => visualDirty.value || sourceDirty.value)

function fieldLabel(field: CPAConfigFieldDefinition): string {
  return t(field.labelZH, field.labelEN)
}

function fieldDescription(field: CPAConfigFieldDefinition): string {
  if (!field.descriptionZH || !field.descriptionEN) {
    return ''
  }
  return t(field.descriptionZH, field.descriptionEN)
}

function scalarValue(field: CPAConfigFieldDefinition): string {
  const value = values[field.key]
  return typeof value === 'string' ? value : ''
}

function booleanValue(field: CPAConfigFieldDefinition): boolean {
  return values[field.key] === true
}

function updateScalar(field: CPAConfigFieldDefinition, value: unknown) {
  values[field.key] = typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}

function updateBoolean(field: CPAConfigFieldDefinition, value: boolean) {
  values[field.key] = value
}

function documentError(content: string): string {
  const document = parseDocument(content)
  return document.errors[0]?.message ?? ''
}

function yamlFragment(value: unknown): string {
  if (value === undefined || value === null) {
    return '[]'
  }
  return stringify(value, { indent: 2 }).trimEnd()
}

function valueAtPath(root: unknown, path: string[]): unknown {
  let current = root
  for (const part of path) {
    if (typeof current !== 'object' || current === null || Array.isArray(current)) {
      return undefined
    }
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function readFieldValue(root: unknown, field: CPAConfigFieldDefinition): ConfigValue {
  let raw = valueAtPath(root, cpaConfigPath(field, apiVersion.value))
  if (apiVersion.value === 0 && field.key === 'rm-panel-repo' && (raw === undefined || raw === null)) {
    raw = valueAtPath(root, ['remote-management', 'panel-repo'])
  }
  switch (field.kind) {
    case 'boolean':
      return typeof raw === 'boolean' ? raw : field.defaultValue === true
    case 'string-list':
    case 'integer-list':
      return Array.isArray(raw) ? raw.map(item => String(item)).join('\n') : ''
    case 'yaml':
      return yamlFragment(raw)
    case 'select':
      if (typeof raw === 'boolean') {
        return String(raw)
      }
      return raw === undefined || raw === null ? String(field.defaultValue ?? '') : String(raw)
    default:
      return raw === undefined || raw === null ? String(field.defaultValue ?? '') : String(raw)
  }
}

function applyContent(content: string) {
  const document = parseDocument(content)
  const error = document.errors[0]
  if (error) {
    throw new Error(error.message)
  }
  const parsed = document.toJS()
  const nextBaseline: Record<string, ConfigValue> = {}
  for (const field of cpaConfigFields) {
    const value = readFieldValue(parsed, field)
    values[field.key] = value
    nextBaseline[field.key] = value
  }
  sourceContent.value = content
  sourceBaseline.value = content
  valueBaseline.value = nextBaseline
}

function lines(value: string): string[] {
  return value.split(/\r?\n/).map(item => item.trim()).filter(Boolean)
}

function writeFieldValue(document: ReturnType<typeof parseDocument>, field: CPAConfigFieldDefinition) {
  if (values[field.key] === valueBaseline.value[field.key]) {
    return
  }
  const value = values[field.key]
  const path = cpaConfigPath(field, apiVersion.value)
  if (field.key === 'rm-panel-repo') {
    document.deleteIn([apiVersion.value === 8 ? 'management' : 'remote-management', 'panel-repo'])
  }
  switch (field.kind) {
    case 'boolean':
      document.setIn(path, value === true)
      break
    case 'number': {
      const text = String(value).trim()
      if (text === '') {
        document.deleteIn(path)
        break
      }
      if (!/^-?\d+$/.test(text) || !Number.isSafeInteger(Number(text))) {
        throw new Error(t(`${field.labelZH}必须是整数`, `${field.labelEN} must be an integer`))
      }
      const number = Number(text)
      if (field.key === 'port' && (number < 1 || number > 65535)) {
        throw new Error(t('监听端口必须在 1 到 65535 之间', 'Listen port must be between 1 and 65535'))
      }
      document.setIn(path, number)
      break
    }
    case 'string-list':
      document.setIn(path, lines(String(value)))
      break
    case 'integer-list': {
      const items = lines(String(value))
      const integers = items.map(item => Number(item))
      if (integers.some(item => !Number.isSafeInteger(item) || item < 0)) {
        throw new Error(t(`${field.labelZH}只能包含非负整数`, `${field.labelEN} may only contain non-negative integers`))
      }
      document.setIn(path, integers)
      break
    }
    case 'yaml': {
      const text = String(value).trim()
      let parsed: unknown
      try {
        parsed = text === '' ? [] : parse(text)
      } catch (error) {
        const detail = error instanceof Error ? error.message : String(error)
        throw new Error(`${fieldLabel(field)}: ${detail}`)
      }
      if (!Array.isArray(parsed)) {
        throw new Error(t(`${field.labelZH}必须是 YAML 数组`, `${field.labelEN} must be a YAML array`))
      }
      document.setIn(path, parsed)
      break
    }
    case 'select':
      if (field.key === 'disable-image-generation' && (value === 'true' || value === 'false')) {
        document.setIn(path, value === 'true')
      } else {
        document.setIn(path, String(value))
      }
      break
    default:
      document.setIn(path, String(value))
  }
}

function composedContent(): string {
  const sourceError = documentError(sourceContent.value)
  if (sourceError) {
    throw new Error(t(`完整 YAML 存在语法错误：${sourceError}`, `The full YAML contains a syntax error: ${sourceError}`))
  }
  const document = parseDocument(sourceContent.value)
  for (const field of cpaConfigFields) {
    writeFieldValue(document, field)
  }
  const content = String(document)
  if (content.trim() === '') {
    throw new Error(t('CPA 配置内容不能为空', 'CPA configuration cannot be empty'))
  }
  const resultError = documentError(content)
  if (resultError) {
    throw new Error(resultError)
  }
  return content
}

async function loadConfig() {
  isLoading.value = true
  loadError.value = ''
  try {
    const response = await getCPAConfig()
    apiVersion.value = response.api_version ?? 0
    revision.value = response.revision ?? ''
    applyContent(response.content)
    isLoaded.value = true
  } catch (error) {
    isLoaded.value = false
    loadError.value = errorText(error, '加载 CPA 配置失败', 'Failed to load CPA configuration')
  } finally {
    isLoading.value = false
  }
}

function validateSettings() {
  if (!isLoaded.value || !isDirty.value) {
    return
  }
  composedContent()
}

async function saveSettings() {
  if (!isLoaded.value || !isDirty.value) {
    return
  }
  const response = await updateCPAConfig({ content: composedContent(), api_version: apiVersion.value, revision: revision.value })
  apiVersion.value = response.api_version ?? 0
  revision.value = response.revision ?? ''
  applyContent(response.content)
}

defineExpose({ saveSettings, validateSettings, reload: loadConfig })

onMounted(loadConfig)
</script>

<template>
  <Card class="cpa-config-panel">
    <CardHeader>
      <div class="min-w-0">
        <CardTitle class="flex items-center gap-2">
          <Settings2Icon class="size-4 text-muted-foreground" />
          {{ t('CPA 配置', 'CPA Configuration') }}
          <Badge v-if="isDirty" variant="secondary">{{ t('未保存', 'Unsaved') }}</Badge>
        </CardTitle>
        <CardDescription>
          {{ t('通过可视化表单或源文件管理 CPA 的完整 config.yaml，未知字段会原样保留。', 'Manage the complete CPA config.yaml visually or as source while preserving unknown fields.') }}
        </CardDescription>
      </div>
      <CardAction>
        <Button size="sm" variant="outline" :disabled="isLoading" @click="loadConfig">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCwIcon v-else data-icon="inline-start" />
          {{ t('重新加载', 'Reload') }}
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent>
      <div v-if="isLoading && !isLoaded" class="cpa-config-skeleton">
        <Skeleton class="h-8 w-full max-w-3xl" />
        <Skeleton class="h-64 w-full" />
      </div>

      <Alert v-else-if="loadError" variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>{{ t('无法读取 CPA 配置', 'Unable to load CPA configuration') }}</AlertTitle>
        <AlertDescription>{{ loadError }}</AlertDescription>
      </Alert>

      <Tabs v-else-if="isLoaded" v-model="activeSection" class="cpa-config-tabs">
        <div class="cpa-config-toolbar">
          <TabsList v-if="editorMode === 'visual'" class="cpa-config-tabs-list">
            <TabsTrigger v-for="section in cpaConfigSections" :key="section.key" :value="section.key">
              {{ t(section.labelZH, section.labelEN) }}
            </TabsTrigger>
          </TabsList>
          <Select v-model="editorMode">
            <SelectTrigger class="cpa-config-mode-select" :aria-label="t('配置编辑模式', 'Configuration editor mode')">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              <SelectGroup>
                <SelectItem value="visual">{{ t('可视化编辑', 'Visual editor') }}</SelectItem>
                <SelectItem value="source">{{ t('源文件编辑', 'Source editor') }}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <template v-if="editorMode === 'visual'">
          <TabsContent
            v-for="section in cpaConfigSections"
            :key="section.key"
            :value="section.key"
            class="cpa-config-tab-content"
          >
            <div class="cpa-config-section-heading">
              <h3>{{ t(section.labelZH, section.labelEN) }}</h3>
              <p>{{ t(section.descriptionZH, section.descriptionEN) }}</p>
            </div>
            <FieldGroup class="cpa-config-grid">
              <template v-for="field in section.fields.filter(item => apiVersion !== 8 || item.key !== 'gemini-cli-endpoint')" :key="field.key">
                <Field
                  v-if="field.kind === 'boolean'"
                  orientation="horizontal"
                  class="cpa-config-switch"
                  :class="{ 'cpa-config-wide': field.wide }"
                >
                  <FieldContent>
                    <FieldTitle>{{ fieldLabel(field) }}</FieldTitle>
                    <FieldDescription v-if="fieldDescription(field)">{{ fieldDescription(field) }}</FieldDescription>
                  </FieldContent>
                  <Switch :model-value="booleanValue(field)" @update:model-value="updateBoolean(field, $event)" />
                </Field>

                <Field v-else :class="{ 'cpa-config-wide': field.wide }">
                  <FieldLabel :for="`cpa-config-${field.key}`">{{ fieldLabel(field) }}</FieldLabel>
                  <Select
                    v-if="field.kind === 'select'"
                    :model-value="scalarValue(field)"
                    @update:model-value="updateScalar(field, $event)"
                  >
                    <SelectTrigger :id="`cpa-config-${field.key}`"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem v-for="option in field.options" :key="option.value" :value="option.value">
                          {{ t(option.labelZH, option.labelEN) }}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <Textarea
                    v-else-if="field.kind === 'string-list' || field.kind === 'integer-list' || field.kind === 'yaml'"
                    :id="`cpa-config-${field.key}`"
                    :model-value="scalarValue(field)"
                    :class="field.kind === 'yaml' ? 'cpa-yaml-fragment' : 'cpa-list-input'"
                    spellcheck="false"
                    @update:model-value="updateScalar(field, $event)"
                  />
                  <Input
                    v-else
                    :id="`cpa-config-${field.key}`"
                    :type="field.secret ? 'password' : field.kind === 'number' ? 'number' : 'text'"
                    :model-value="scalarValue(field)"
                    :placeholder="field.placeholder"
                    @update:model-value="updateScalar(field, $event)"
                  />
                  <FieldDescription v-if="fieldDescription(field)">{{ fieldDescription(field) }}</FieldDescription>
                </Field>
              </template>
            </FieldGroup>
          </TabsContent>
        </template>

        <div v-else class="cpa-config-tab-content">
          <Alert class="mb-4">
            <FileCode2Icon />
            <AlertTitle>{{ t('完整配置源码', 'Complete configuration source') }}</AlertTitle>
            <AlertDescription>
              {{ t('这里包含 CPA 当前与未来版本的全部配置项。保存前会校验 YAML，但具体参数范围仍由 CPA 后端校验。', 'This contains every setting from current and future CPA versions. YAML is validated before saving; CPA still validates parameter ranges.') }}
            </AlertDescription>
          </Alert>
          <Textarea
            v-model="sourceContent"
            class="cpa-config-source"
            spellcheck="false"
            :aria-label="t('CPA 完整 YAML 配置', 'Complete CPA YAML configuration')"
          />
        </div>
      </Tabs>
    </CardContent>
  </Card>
</template>

<style scoped>
.cpa-config-skeleton {
  display: grid;
  gap: 1rem;
}

.cpa-config-tabs {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 1rem;
}

.cpa-config-toolbar {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: .75rem;
}

.cpa-config-tabs-list {
  width: fit-content;
  max-width: calc(100% - 11rem);
  justify-content: flex-start;
  overflow-x: auto;
  overflow-y: hidden;
}

.cpa-config-mode-select {
  width: 10rem;
  margin-left: auto;
  flex: 0 0 auto;
}

.cpa-config-tabs-list :deep([data-slot="tabs-trigger"]) {
  flex: 0 0 auto;
  padding-inline: .75rem;
}

.cpa-config-tab-content {
  min-width: 0;
}

.cpa-config-section-heading {
  margin-bottom: 1rem;
}

.cpa-config-section-heading h3 {
  margin: 0;
  color: var(--foreground);
  font-size: .9375rem;
  font-weight: 600;
}

.cpa-config-section-heading p {
  margin: .25rem 0 0;
  color: var(--muted-foreground);
  font-size: .8125rem;
}

.cpa-config-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}

.cpa-config-wide {
  grid-column: 1 / -1;
}

.cpa-config-switch {
  min-height: 4.75rem;
  padding: .875rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--cpa-surface-muted);
}

.cpa-list-input {
  min-height: 7rem;
  font-family: var(--font-mono, ui-monospace, monospace);
}

.cpa-yaml-fragment {
  min-height: 12rem;
  font-family: var(--font-mono, ui-monospace, monospace);
  white-space: pre;
}

.cpa-config-source {
  min-height: 34rem;
  max-height: 72vh;
  overflow: auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  line-height: 1.55;
  white-space: pre;
}

@media (max-width: 800px) {
  .cpa-config-toolbar {
    flex-wrap: wrap;
  }

  .cpa-config-tabs-list {
    max-width: 100%;
  }

  .cpa-config-grid {
    grid-template-columns: 1fr;
  }

  .cpa-config-wide {
    grid-column: auto;
  }
}
</style>

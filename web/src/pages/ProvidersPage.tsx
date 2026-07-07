import { useState, useMemo, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import {
  Plus,
  Pencil,
  Trash2,
  Eye,
  EyeOff,
  Boxes,
  Download,
  MoreHorizontal,
  GitBranch,
  X,
  Search,
} from 'lucide-react'
import { Modal } from '@/components/ui/Modal'
import { GlassCard } from '@/components/ui/GlassCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Toggle } from '@/components/ui/Toggle'
import { Badge } from '@/components/ui/Badge'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  listProviderModels,
  createProviderModel,
  deleteProviderModel,
  importProviderModels,
  fetchProviderModels,
  listProviderModelAliases,
  createModelAlias,
  updateModelAlias,
  deleteModelAlias,
  fetchProviderModelsPreview,
} from '@/api/client'
import type { Provider, ProviderType, ProviderModel, ModelAlias } from '@/types'

const PROVIDER_TYPES: { value: ProviderType; label: string }[] = [
  { value: 'openai-chat-completions', label: 'OpenAI Chat Completions' },
  { value: 'openai-responses', label: 'OpenAI Responses' },
  { value: 'anthropic-messages', label: 'Anthropic Messages' },
]

// ─── Temporary model item for new provider ──────────────────────────────────

interface TempModel {
  id: string
  model_name: string
  enabled: boolean
  source: 'manual' | 'upstream'
}

let tempIdCounter = 0
function nextTempId(): string {
  tempIdCounter += 1
  return `temp_${tempIdCounter}`
}

// ─── Provider Create/Edit Modal ──────────────────────────────────────────────

function ProviderFormModal({
  provider,
  onClose,
}: {
  provider?: Provider
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [name, setName] = useState(provider?.name ?? '')
  const [type, setType] = useState<ProviderType>(provider?.type ?? 'openai-chat-completions')
  const [baseUrl, setBaseUrl] = useState(provider?.base_url ?? '')
  const [apiKey, setApiKey] = useState(provider?.api_key ?? '')
  const [enabled, setEnabled] = useState(provider?.enabled ?? true)
  const [showKey, setShowKey] = useState(false)
  const [error, setError] = useState('')

  // Model management
  const [tempModels, setTempModels] = useState<TempModel[]>([])
  const [newModelName, setNewModelName] = useState('')
  const [showFetchModal, setShowFetchModal] = useState(false)

  // For edit: load existing models
  const { data: existingModels = [], isLoading: modelsLoading } = useQuery({
    queryKey: ['provider-models', provider?.id],
    queryFn: () => listProviderModels(provider!.id),
    enabled: !!provider,
  })

  const isEditing = !!provider

  // ── Mutations ──

  const createMut = useMutation({
    mutationFn: () =>
      createProvider({
        name,
        type,
        base_url: baseUrl,
        api_key: apiKey,
        enabled,
        model_prefix: '',
        hide_original_models: false,
      }),
    onSuccess: async (createdProvider) => {
      // Batch import temp models, split by source
      if (tempModels.length > 0) {
        // Manual models: one API call each, parallel
        const manualModels = tempModels.filter((m) => m.source === 'manual')
        if (manualModels.length > 0) {
          try {
            await Promise.all(
              manualModels.map((m) =>
                createProviderModel(createdProvider.id, { model_name: m.model_name }),
              ),
            )
          } catch {
            // non-fatal: models may partially import
          }
        }

        // Upstream models: single bulk import call
        const upstreamModels = tempModels.filter((m) => m.source === 'upstream')
        if (upstreamModels.length > 0) {
          try {
            await importProviderModels(createdProvider.id, {
              models: upstreamModels.map((m) => ({
                model_name: m.model_name,
                enabled: m.enabled,
              })),
            })
          } catch {
            // non-fatal: models may partially import
          }
        }
      }
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      onClose()
    },
    onError: (err: any) => setError(err.message),
  })

  const updateMut = useMutation({
    mutationFn: () =>
      updateProvider(provider!.id, {
        name,
        type,
        base_url: baseUrl,
        api_key: apiKey,
        enabled,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      onClose()
    },
    onError: (err: any) => setError(err.message),
  })

  const createModelMut = useMutation({
    mutationFn: (modelName: string) =>
      createProviderModel(provider!.id, { model_name: modelName }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['provider-models', provider!.id] })
      setNewModelName('')
    },
    onError: (err: any) => setError(err.message),
  })

  const deleteModelMut = useMutation({
    mutationFn: (modelId: string) => deleteProviderModel(provider!.id, modelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['provider-models', provider!.id] })
    },
    onError: (err: any) => setError(err.message),
  })

  // ── Handlers ──

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name || !baseUrl || !apiKey) {
      setError(t('providers.fillRequired'))
      return
    }
    if (isEditing) {
      updateMut.mutate()
    } else {
      createMut.mutate()
    }
  }

  const handleAddTempModel = () => {
    const modelName = newModelName.trim()
    if (!modelName) return
    if (tempModels.some((m) => m.model_name === modelName)) return
    setTempModels((prev) => [
      ...prev,
      { id: nextTempId(), model_name: modelName, enabled: true, source: 'manual' },
    ])
    setNewModelName('')
  }

  const handleDeleteTempModel = (id: string) => {
    setTempModels((prev) => prev.filter((m) => m.id !== id))
  }

  const handleImportTempModels = (models: string[]) => {
    setTempModels((prev) => {
      const existingNames = new Set(prev.map((m) => m.model_name))
      const newOnes = models
        .filter((name) => !existingNames.has(name))
        .map((name) => ({ id: nextTempId(), model_name: name, enabled: true, source: 'upstream' as const }))
      return [...prev, ...newOnes]
    })
  }

  const loading = createMut.isPending || updateMut.isPending

  // ── Render ──

  const modelList = isEditing ? existingModels : tempModels

  return (
    <Modal
      title={isEditing ? t('providers.editProvider') : t('providers.newProvider')}
      onClose={onClose}
      maxWidth="sm"
      zIndex="z-[100]"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
              {/* Name */}
              <Input
                label={t('providers.providerName')}
                placeholder={t('providers.providerNamePlaceholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
              />

              {/* Type */}
              <Select
                label={t('providers.providerType')}
                options={PROVIDER_TYPES}
                value={type}
                onChange={(e) => setType(e.target.value as ProviderType)}
              />

              {/* Base URL */}
              <Input
                label={t('providers.baseUrl')}
                placeholder="https://api.openai.com/v1"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
              />

              {/* API Key */}
              <div className="relative">
                <Input
                  label={t('providers.apiKey')}
                  type={showKey ? 'text' : 'password'}
                  placeholder="sk-..."
                  value={apiKey}
                  autoComplete="new-password"
                  onChange={(e) => setApiKey(e.target.value)}
                  className={apiKey ? 'pr-10' : ''}
                />
                {apiKey && (
                  <button
                    type="button"
                    onClick={() => setShowKey(!showKey)}
                    className="absolute right-3 bottom-[13px] text-text-muted hover:text-text-primary transition-colors"
                  >
                    {showKey ? (
                      <EyeOff className="w-4 h-4" />
                    ) : (
                      <Eye className="w-4 h-4" />
                    )}
                  </button>
                )}
              </div>

              {/* Model List */}
              <div className="space-y-3">
                <label className="block text-sm font-medium text-text-secondary">
                  {t('providers.models')}
                </label>

                {/* Add model row */}
                <div className="flex gap-2">
                  <Input
                    placeholder={t('providers.addModelPlaceholder')}
                    value={newModelName}
                    onChange={(e) => setNewModelName(e.target.value)}
                    className="flex-1"
                  />
                  <Button
                    type="button"
                    onClick={isEditing ? () => createModelMut.mutate(newModelName.trim()) : handleAddTempModel}
                    loading={isEditing ? createModelMut.isPending : false}
                    disabled={!newModelName.trim()}
                  >
                    <Plus className="w-4 h-4" />
                    {t('providers.addModelBtn')}
                  </Button>
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => setShowFetchModal(true)}
                  >
                    <Download className="w-4 h-4" />
                    {t('providers.fetchModels')}
                  </Button>
                </div>

                {/* Model items */}
                {modelsLoading ? (
                  <div className="space-y-2">
                    {[...Array(2)].map((_, i) => (
                      <div key={i} className="h-12 rounded-xl bg-card animate-pulse" />
                    ))}
                  </div>
                ) : modelList.length === 0 ? (
                  <div className="text-center py-6 text-text-muted">
                    <Boxes className="w-8 h-8 mx-auto mb-1" />
                    <p className="text-sm">{t('providers.noModels')}</p>
                  </div>
                ) : (
                  <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto">
                    {modelList.map((model) => {
                      const pm = model as ProviderModel
                      const tm = model as TempModel
                      const isPersisted = 'provider_id' in pm
                      const modelId = isPersisted ? pm.id : tm.id
                      const modelName = isPersisted ? pm.model_name : tm.model_name
                      const isManual = isPersisted
                        ? pm.source === 'manual'
                        : tm.source === 'manual'
                      return (
                        <span
                          key={modelId}
                          className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-card-border bg-surface-light px-2 py-1 text-xs text-text-primary"
                        >
                          <span className="truncate max-w-[220px]">{modelName}</span>
                          {isManual && (
                            <span className="rounded-md border border-red-500/60 bg-red-500/10 px-1.5 py-0.5 text-[10px] font-medium text-red-600 dark:text-red-300">
                              {t('providers.manual')}
                            </span>
                          )}
                          <button
                            type="button"
                            onClick={() => {
                              if (isPersisted) {
                                deleteModelMut.mutate(pm.id)
                              } else {
                                handleDeleteTempModel(tm.id)
                              }
                            }}
                            className="rounded-md p-0.5 text-text-muted hover:text-red-600 dark:hover:text-red-300 hover:bg-red-500/10 transition-colors"
                            title={t('providers.deleteModel')}
                          >
                            <X className="w-3 h-3" />
                          </button>
                        </span>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Enabled toggle */}
              <div className="flex items-center gap-3 pt-2">
                <Toggle
                  enabled={enabled}
                  onChange={setEnabled}
                  label={t('providers.enabled')}
                />
              </div>

              {error && (
                <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">
                  {error}
                </p>
              )}

              <div className="flex justify-end gap-3 pt-2">
                <Button variant="secondary" onClick={onClose} type="button">
                  {t('providers.cancel')}
                </Button>
                <Button type="submit" loading={loading}>
                  {isEditing ? t('providers.saveProvider') : t('providers.createProvider')}
                </Button>
              </div>
      </form>

      {/* Fetch Models Sub-Modal */}
      {showFetchModal && (
        <FetchModelsModal
          provider={provider}
          previewParams={isEditing ? undefined : { type, base_url: baseUrl, api_key: apiKey }}
          existingModels={isEditing ? existingModels : tempModels}
          onImport={handleImportTempModels}
          onClose={() => setShowFetchModal(false)}
        />
      )}
    </Modal>
  )
}

// ─── Fetch Models Sub-Modal ──────────────────────────────────────────────────

function FetchModelsModal({
  provider,
  previewParams,
  existingModels,
  onImport,
  onClose,
}: {
  provider?: Provider
  previewParams?: { type: string; base_url: string; api_key: string }
  existingModels: (ProviderModel | TempModel)[]
  onImport: (models: string[]) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [fetchedModels, setFetchedModels] = useState<string[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [fetching, setFetching] = useState(false)
  const [importing, setImporting] = useState(false)
  const [fetchError, setFetchError] = useState('')
  const [search, setSearch] = useState('')
  const autoFetchStartedRef = useRef(false)

  const existingNames = useMemo(
    () => new Set(existingModels.map((m) => m.model_name)),
    [existingModels],
  )

  const filteredModels = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return fetchedModels
    return fetchedModels.filter((name) => name.toLowerCase().includes(q))
  }, [fetchedModels, search])

  const handleFetch = async () => {
    setFetching(true)
    setFetchError('')
    try {
      let models: string[]
      if (provider) {
        // Existing provider: use existing fetch API
        models = await fetchProviderModels(provider.id)
      } else if (previewParams) {
        // New provider: use preview API
        models = await fetchProviderModelsPreview(previewParams)
      } else {
        setFetchError(t('providers.fillRequired'))
        return
      }
      setFetchedModels(models)
      setSelected(new Set())
    } catch (err: any) {
      setFetchError(err.message || t('providers.fetchError'))
    } finally {
      setFetching(false)
    }
  }

  useEffect(() => {
    if (autoFetchStartedRef.current) return
    autoFetchStartedRef.current = true
    handleFetch()
  }, [])

  const toggleModel = (name: string) => {
    if (existingNames.has(name)) return
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      return next
    })
  }

  const handleImport = async () => {
    if (selected.size === 0) return
    const models = Array.from(selected)
    setImporting(true)
    setFetchError('')
    try {
      if (provider) {
        await importProviderModels(provider.id, {
          models: models.map((name) => ({ model_name: name })),
        })
        queryClient.invalidateQueries({ queryKey: ['provider-models', provider.id] })
      } else {
        onImport(models)
      }
      onClose()
    } catch (err: any) {
      setFetchError(err.message || t('providers.importError'))
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal
      title={t('providers.fetchModelsTitle')}
      onClose={onClose}
      maxWidth="lg"
      zIndex="z-[110]"
    >
      <p className="text-sm text-text-secondary mb-6">
        {t('providers.fetchModelsDesc')}
      </p>

      <div className="flex gap-6">
              {/* Left: Actions */}
              <div className="w-64 shrink-0 space-y-4">
                <Button onClick={handleFetch} loading={fetching} className="w-full">
                  <Download className="w-4 h-4" />
                  {fetching ? t('providers.fetching') : t('providers.refetchBtn')}
                </Button>

                {fetchedModels.length > 0 && (
                  <Button
                    onClick={handleImport}
                    loading={importing}
                    disabled={selected.size === 0}
                    variant="primary"
                    className="w-full"
                  >
                    {t('providers.importBtn')} ({selected.size})
                  </Button>
                )}

                {fetchError && (
                  <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">
                    {fetchError}
                  </p>
                )}
              </div>

              {/* Right: Model List */}
              <div className="flex-1 min-w-0">
                {fetching && fetchedModels.length === 0 ? (
                  <div className="h-48 flex flex-col items-center justify-center text-text-muted text-sm">
                    <div className="w-5 h-5 border-2 border-text-muted border-t-transparent rounded-full animate-spin mb-2" />
                    {t('providers.fetching')}
                  </div>
                ) : fetchedModels.length === 0 && !fetching ? (
                  <div className="h-48 flex items-center justify-center text-text-muted text-sm">
                    {t('providers.noFetchedModels')}
                  </div>
                ) : (
                  <>
                    {/* Search */}
                    <div className="relative mb-3">
                      <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted pointer-events-none" />
                      <input
                        type="text"
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        placeholder={t('providers.searchModelsPlaceholder')}
                        aria-label={t('providers.searchModelsPlaceholder')}
                        className="w-full pl-10 pr-9 py-2 rounded-xl bg-surface-light border border-surface-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                      />
                      {search && (
                        <button
                          type="button"
                          onClick={() => setSearch('')}
                          aria-label={t('common.clear')}
                          className="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded-md text-text-muted hover:text-text-primary hover:bg-card transition-colors"
                        >
                          <X className="w-3.5 h-3.5" />
                        </button>
                      )}
                    </div>

                    {filteredModels.length === 0 ? (
                      <div className="h-48 flex items-center justify-center text-text-muted text-sm">
                        {t('common.noResults')}
                      </div>
                    ) : (
                      <div className="max-h-80 overflow-y-auto space-y-1.5 pr-2">
                        {filteredModels.map((name) => {
                          const isExisting = existingNames.has(name)
                          const isSelected = selected.has(name)
                          return (
                            <label
                              key={name}
                              className={`
                                flex items-center gap-3 px-3 py-2 rounded-lg cursor-pointer
                                transition-colors
                                ${isExisting
                                  ? 'bg-card text-text-muted cursor-not-allowed'
                                  : isSelected
                                    ? 'bg-accent/20 text-text-primary'
                                    : 'hover:bg-card text-text-secondary'
                                }
                              `}
                            >
                              <input
                                type="checkbox"
                                checked={isSelected || isExisting}
                                disabled={isExisting}
                                onChange={() => toggleModel(name)}
                                className="accent-accent"
                              />
                              <span className="flex-1 truncate text-sm">{name}</span>
                              {isExisting && (
                                <Badge variant="info">{t('providers.alreadyAdded')}</Badge>
                              )}
                            </label>
                          )
                        })}
                      </div>
                    )}
                  </>
                )}
              </div>
      </div>

      <div className="flex justify-end mt-6">
        <Button variant="secondary" onClick={onClose}>
          {t('providers.cancel')}
        </Button>
      </div>
    </Modal>
  )
}

// ─── Model Alias Modal ───────────────────────────────────────────────────────

function ModelAliasModal({
  provider,
  onClose,
}: {
  provider: Provider
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // Extra model prefix & hide original models
  const [modelPrefix, setModelPrefix] = useState(provider.model_prefix ?? '')
  const [hideOriginal, setHideOriginal] = useState(provider.hide_original_models ?? false)
  const [prefixError, setPrefixError] = useState('')

  // Model aliases
  const { data: providerAliases = [], isLoading: aliasesLoading } = useQuery({
    queryKey: ['provider-model-aliases', provider.id],
    queryFn: () => listProviderModelAliases(provider.id),
  })

  // Provider models for datalist
  const { data: providerModels = [] } = useQuery({
    queryKey: ['provider-models', provider.id],
    queryFn: () => listProviderModels(provider.id),
  })

  // Alias form
  const [aliasFormVisible, setAliasFormVisible] = useState(false)
  const [editingAlias, setEditingAlias] = useState<ModelAlias | undefined>()
  const [aliasName, setAliasName] = useState('')
  const [aliasTarget, setAliasTarget] = useState('')
  const [aliasEnabled, setAliasEnabled] = useState(true)
  const [aliasError, setAliasError] = useState('')

  const updatePrefixMut = useMutation({
    mutationFn: () =>
      updateProvider(provider.id, {
        model_prefix: modelPrefix,
        hide_original_models: hideOriginal,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      setPrefixError('')
    },
    onError: (err: any) => setPrefixError(err.message),
  })

  const createAliasMut = useMutation({
    mutationFn: () =>
      createModelAlias({
        alias: aliasName,
        provider_id: provider.id,
        target_model: aliasTarget,
        enabled: aliasEnabled,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['provider-model-aliases', provider.id] })
      resetAliasForm()
    },
    onError: (err: any) => setAliasError(err.message),
  })

  const updateAliasMut = useMutation({
    mutationFn: () =>
      updateModelAlias(editingAlias!.id, {
        alias: aliasName,
        target_model: aliasTarget,
        enabled: aliasEnabled,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['provider-model-aliases', provider.id] })
      resetAliasForm()
    },
    onError: (err: any) => setAliasError(err.message),
  })

  const deleteAliasMut = useMutation({
    mutationFn: (id: string) => deleteModelAlias(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['provider-model-aliases', provider.id] })
    },
  })

  const resetAliasForm = () => {
    setAliasFormVisible(false)
    setEditingAlias(undefined)
    setAliasName('')
    setAliasTarget('')
    setAliasEnabled(true)
    setAliasError('')
  }

  const openNewAlias = () => {
    setEditingAlias(undefined)
    setAliasName('')
    setAliasTarget('')
    setAliasEnabled(true)
    setAliasError('')
    setAliasFormVisible(true)
  }

  const openEditAlias = (alias: ModelAlias) => {
    setEditingAlias(alias)
    setAliasName(alias.alias)
    setAliasTarget(alias.target_model)
    setAliasEnabled(alias.enabled)
    setAliasError('')
    setAliasFormVisible(true)
  }

  const handleAliasSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setAliasError('')
    if (!aliasName || !aliasTarget) {
      setAliasError(t('providers.fillRequired'))
      return
    }
    if (editingAlias) {
      updateAliasMut.mutate()
    } else {
      createAliasMut.mutate()
    }
  }

  return (
    <Modal
      title={t('providers.modelAliasTitle')}
      onClose={onClose}
      maxWidth="md"
      zIndex="z-[100]"
    >
      {/* Section A: Extra Model Prefix & Hide Original Models */}
      <div className="space-y-4 pb-6 border-b border-card-border mb-6">
              <h3 className="text-sm font-semibold text-text-primary">
                {t('providers.extraModelPrefix')}
              </h3>
              <Input
                label={t('providers.modelPrefix')}
                placeholder={t('providers.modelPrefixPlaceholder')}
                value={modelPrefix}
                onChange={(e) => setModelPrefix(e.target.value)}
              />
              <div className="flex items-center gap-3">
                <Toggle
                  enabled={hideOriginal}
                  onChange={setHideOriginal}
                  label={t('providers.hideOriginalModels')}
                />
              </div>
              {prefixError && (
                <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">
                  {prefixError}
                </p>
              )}
              <Button
                type="button"
                onClick={() => updatePrefixMut.mutate()}
                loading={updatePrefixMut.isPending}
                size="sm"
              >
                {t('providers.savePrefix')}
              </Button>
            </div>

            {/* Section B: Model Aliases */}
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold text-text-primary">
                  {t('providers.modelAliases')}
                </h3>
                {!aliasFormVisible && (
                  <Button onClick={openNewAlias} size="sm">
                    <Plus className="w-4 h-4" />
                    {t('providers.newAlias')}
                  </Button>
                )}
              </div>

              {/* Alias form */}
              {aliasFormVisible && (
                <form onSubmit={handleAliasSubmit} className="space-y-3 p-4 rounded-xl bg-card border border-card-border">
                  <Input
                    label={t('providers.aliasName')}
                    placeholder={t('providers.aliasNamePlaceholder')}
                    value={aliasName}
                    onChange={(e) => setAliasName(e.target.value)}
                    autoFocus
                  />
                  <div className="space-y-1.5">
                    <label className="block text-sm font-medium text-text-secondary">
                      {t('providers.targetModel')}
                    </label>
                    <input
                      list="alias-target-model-list"
                      className="w-full rounded-xl border border-surface-border bg-surface-light px-4 py-2.5 text-sm text-text-primary placeholder:text-text-muted transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent hover:border-accent/30"
                      placeholder={t('providers.selectProviderModel')}
                      value={aliasTarget}
                      onChange={(e) => setAliasTarget(e.target.value)}
                    />
                    <datalist id="alias-target-model-list">
                      {providerModels.map((pm) => (
                        <option key={pm.id} value={pm.model_name} />
                      ))}
                    </datalist>
                  </div>
                  <div className="flex items-center gap-3">
                    <Toggle
                      enabled={aliasEnabled}
                      onChange={setAliasEnabled}
                      label={t('providers.enabled')}
                    />
                  </div>
                  {aliasError && (
                    <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">
                      {aliasError}
                    </p>
                  )}
                  <div className="flex justify-end gap-2">
                    <Button variant="secondary" type="button" onClick={resetAliasForm} size="sm">
                      {t('providers.cancel')}
                    </Button>
                    <Button type="submit" loading={createAliasMut.isPending || updateAliasMut.isPending} size="sm">
                      {editingAlias ? t('providers.saveAlias') : t('providers.createAlias')}
                    </Button>
                  </div>
                </form>
              )}

              {/* Alias list */}
              {aliasesLoading ? (
                <div className="space-y-2">
                  {[...Array(2)].map((_, i) => (
                    <div key={i} className="h-14 rounded-xl bg-card animate-pulse" />
                  ))}
                </div>
              ) : providerAliases.length === 0 ? (
                <div className="text-center py-8 text-text-muted">
                  <GitBranch className="w-8 h-8 mx-auto mb-1" />
                  <p className="text-sm">{t('providers.noAliases')}</p>
                </div>
              ) : (
                <div className="space-y-2">
                  {providerAliases.map((alias) => (
                    <div
                      key={alias.id}
                      className="flex items-center justify-between px-4 py-3 rounded-xl bg-card border border-card-border"
                    >
                      <div className="flex items-center gap-3 min-w-0">
                        <div className="p-1.5 rounded-lg bg-accent/20">
                          <GitBranch className="w-4 h-4 text-accent-light" />
                        </div>
                        <div className="min-w-0">
                          <p className="text-sm text-text-primary font-medium">
                            {alias.alias}
                          </p>
                          <p className="text-xs text-text-secondary truncate">
                            {t('providers.mappedTo')}: {alias.target_model}
                          </p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        <Toggle
                          enabled={alias.enabled}
                          onChange={async (v) => {
                            try {
                              await updateModelAlias(alias.id, { enabled: v })
                              queryClient.invalidateQueries({ queryKey: ['provider-model-aliases', provider.id] })
                            } catch {}
                          }}
                        />
                        <button
                          type="button"
                          onClick={() => openEditAlias(alias)}
                          className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
                        >
                          <Pencil className="w-4 h-4" />
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            if (confirm(t('providers.deleteAliasConfirm'))) {
                              deleteAliasMut.mutate(alias.id)
                            }
                          }}
                          className="p-1.5 rounded-lg text-text-muted hover:text-red-600 dark:hover:text-red-300 hover:bg-red-500/10 transition-colors"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
      </div>
    </Modal>
  )
}

// ─── More Menu ───────────────────────────────────────────────────────────────

function MoreMenu({
  provider,
  onOpenAliases,
  onDelete,
}: {
  provider: Provider
  onOpenAliases: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (!target.closest('[data-more-menu]')) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="relative" data-more-menu>
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
      >
        <MoreHorizontal className="w-4 h-4" />
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 w-44 rounded-xl bg-surface-light border border-card-border shadow-lg py-1">
          <button
            type="button"
            onClick={() => {
              setOpen(false)
              onOpenAliases()
            }}
            className="w-full flex items-center gap-2 px-4 py-2 text-sm text-text-primary hover:bg-card transition-colors text-left"
          >
            <GitBranch className="w-4 h-4" />
            {t('providers.modelAliases')}
          </button>
          <hr className="border-card-border mx-2" />
          <button
            type="button"
            onClick={() => {
              setOpen(false)
              onDelete()
            }}
            className="w-full flex items-center gap-2 px-4 py-2 text-sm text-red-600 dark:text-red-300 hover:bg-red-500/10 transition-colors text-left"
          >
            <Trash2 className="w-4 h-4" />
            {t('providers.deleteProvider')}
          </button>
        </div>
      )}
    </div>
  )
}

// ─── Main Providers Page ─────────────────────────────────────────────────────

export function ProvidersPage() {
  const { t } = useTranslation()
  const [showModal, setShowModal] = useState(false)
  const [editingProvider, setEditingProvider] = useState<Provider | undefined>()
  const [aliasModalProvider, setAliasModalProvider] = useState<Provider | undefined>()
  const queryClient = useQueryClient()

  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteProvider(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
    },
  })

  const openEdit = (provider: Provider) => {
    setEditingProvider(provider)
    setShowModal(true)
  }

  const openCreate = () => {
    setEditingProvider(undefined)
    setShowModal(true)
  }

  const handleDelete = (provider: Provider) => {
    if (confirm(t('providers.deleteConfirm'))) {
      deleteMut.mutate(provider.id)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">{t('providers.title')}</h1>
          <p className="text-text-secondary mt-1">{t('providers.description')}</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="w-4 h-4" />
          {t('providers.newProvider')}
        </Button>
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-40 rounded-2xl bg-card animate-pulse" />
          ))}
        </div>
      ) : providers && providers.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {providers.map((provider) => (
            <div key={provider.id}>
              <GlassCard className="p-5">
                <div className="flex items-start justify-between mb-3">
                  <div>
                    <h3 className="text-text-primary font-semibold">{provider.name}</h3>
                    <Badge variant="info">{provider.type}</Badge>
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => openEdit(provider)}
                      className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
                      title={t('providers.editProvider')}
                    >
                      <Pencil className="w-4 h-4" />
                    </button>
                    <MoreMenu
                      provider={provider}
                      onOpenAliases={() => setAliasModalProvider(provider)}
                      onDelete={() => handleDelete(provider)}
                    />
                  </div>
                </div>
                <div className="space-y-1 text-sm text-text-secondary">
                  <p className="truncate">
                    <span className="text-text-muted">URL: </span>
                    {provider.base_url}
                  </p>
                  <p>
                    <span className="text-text-muted">Key: </span>
                    {provider.api_key.slice(0, 8)}...
                  </p>
                  {provider.model_prefix && (
                    <p>
                      <span className="text-text-muted">{t('providers.modelPrefix')}: </span>
                      {provider.model_prefix}
                    </p>
                  )}
                </div>
                <div className="mt-3 flex items-center gap-2">
                  <Toggle
                    enabled={provider.enabled}
                    onChange={async (v) => {
                      try {
                        await updateProvider(provider.id, { enabled: v })
                        queryClient.invalidateQueries({ queryKey: ['providers'] })
                      } catch {}
                    }}
                  />
                  <span className="text-xs text-text-muted">
                    {provider.enabled ? t('providers.enabled') : t('providers.disabled')}
                  </span>
                </div>
              </GlassCard>
            </div>
          ))}
        </div>
      ) : (
        <GlassCard className="p-8 text-center">
          <Boxes className="w-12 h-12 mx-auto mb-3 text-text-muted" />
          <p className="text-text-secondary">{t('providers.noProviders')}</p>
          <p className="text-text-muted text-sm mt-1">{t('providers.noProvidersDesc')}</p>
        </GlassCard>
      )}

      {showModal && (
        <ProviderFormModal
          provider={editingProvider}
          onClose={() => setShowModal(false)}
        />
      )}

      {aliasModalProvider && (
        <ModelAliasModal
          provider={aliasModalProvider}
          onClose={() => setAliasModalProvider(undefined)}
        />
      )}
    </div>
  )
}

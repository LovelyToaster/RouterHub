import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Plus, Pencil, Trash2, Eye, EyeOff, Key, Copy } from 'lucide-react'
import { Modal } from '@/components/ui/Modal'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { GlassCard } from '@/components/ui/GlassCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Toggle } from '@/components/ui/Toggle'
import { Badge } from '@/components/ui/Badge'
import {
  listGatewayKeys,
  createGatewayKey,
  updateGatewayKey,
  deleteGatewayKey,
} from '@/api/client'
import type { GatewayAPIKey } from '@/types'

// ─── Create Modal ─────────────────────────────────────────────────────────

function CreateKeyModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [error, setError] = useState('')

  const createMut = useMutation({
    mutationFn: () => createGatewayKey({ name: name.trim(), enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateway-keys'] })
      onClose()
    },
    onError: (err: any) => setError(err.message),
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError(t('apiKeys.fillRequired'))
      return
    }
    createMut.mutate()
  }

  return (
    <Modal
      title={t('apiKeys.createTitle')}
      onClose={onClose}
      maxWidth="sm"
      zIndex="z-[100]"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <Input
          label={t('apiKeys.name')}
          placeholder={t('apiKeys.namePlaceholder')}
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />

        <div className="flex items-center gap-3 pt-2">
          <Toggle enabled={enabled} onChange={setEnabled} label={t('apiKeys.enabled')} />
        </div>

        {error && (
          <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{error}</p>
        )}

        <div className="flex justify-end gap-3 pt-2">
          <Button variant="secondary" onClick={onClose} type="button">
            {t('apiKeys.cancel')}
          </Button>
          <Button type="submit" loading={createMut.isPending}>
            {t('apiKeys.create')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

// ─── Edit Modal ────────────────────────────────────────────────────────────

function EditKeyModal({ keyItem, onClose }: { keyItem: GatewayAPIKey; onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState(keyItem.name)
  const [enabled, setEnabled] = useState(keyItem.enabled)
  const [error, setError] = useState('')

  const updateMut = useMutation({
    mutationFn: () => updateGatewayKey(keyItem.id, { name: name.trim(), enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateway-keys'] })
      onClose()
    },
    onError: (err: any) => setError(err.message),
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError(t('apiKeys.fillRequired'))
      return
    }
    updateMut.mutate()
  }

  return (
    <Modal
      title={t('apiKeys.editTitle')}
      onClose={onClose}
      maxWidth="sm"
      zIndex="z-[100]"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <Input
          label={t('apiKeys.name')}
          placeholder={t('apiKeys.namePlaceholder')}
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />

        <div>
          <label className="block text-sm font-medium text-text-secondary mb-1.5">
            {t('apiKeys.apiKey')}
          </label>
          <code className="block rounded-xl border border-surface-border bg-surface-light px-3 py-2 text-sm font-mono text-text-secondary break-all">
            {keyItem.api_key}
          </code>
        </div>

        <div className="flex items-center gap-3 pt-2">
          <Toggle enabled={enabled} onChange={setEnabled} label={t('apiKeys.enabled')} />
        </div>

        {error && (
          <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{error}</p>
        )}

        <div className="flex justify-end gap-3 pt-2">
          <Button variant="secondary" onClick={onClose} type="button">
            {t('apiKeys.cancel')}
          </Button>
          <Button type="submit" loading={updateMut.isPending}>
            {t('apiKeys.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

// ─── Page ──────────────────────────────────────────────────────────────────

function maskKey(key: string): string {
  if (key.length <= 12) return key
  return key.slice(0, 6) + '•'.repeat(Math.min(20, key.length - 10)) + key.slice(-4)
}

export function ApiKeysPage() {
  const { t } = useTranslation()
  const [showCreate, setShowCreate] = useState(false)
  const [editingKey, setEditingKey] = useState<GatewayAPIKey | undefined>()
  const [visibleKeys, setVisibleKeys] = useState<Set<string>>(new Set())
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const { data: keys, isLoading } = useQuery({
    queryKey: ['gateway-keys'],
    queryFn: listGatewayKeys,
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteGatewayKey(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateway-keys'] })
      setDeleteConfirmId(null)
    },
  })

  const toggleVisible = (id: string) => {
    setVisibleKeys((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const copyKey = async (id: string, key: string) => {
    try {
      await navigator.clipboard.writeText(key)
      setCopiedId(id)
      setTimeout(() => setCopiedId((cur) => (cur === id ? null : cur)), 1500)
    } catch {
      // clipboard blocked
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">{t('apiKeys.title')}</h1>
          <p className="text-text-secondary mt-1">{t('apiKeys.subtitle')}</p>
        </div>
        <Button onClick={() => setShowCreate(true)}>
          <Plus className="w-4 h-4" />
          {t('apiKeys.newKey')}
        </Button>
      </div>

      {isLoading ? (
        <div className="space-y-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-24 rounded-2xl bg-card animate-pulse" />
          ))}
        </div>
      ) : keys && keys.length > 0 ? (
        <div className="space-y-3">
          {keys.map((keyItem) => {
            const visible = visibleKeys.has(keyItem.id)
            const copied = copiedId === keyItem.id
            return (
              <div key={keyItem.id}>
                <GlassCard className="p-4" hover>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4 flex-1 min-w-0">
                      <div className="p-2 rounded-xl bg-amber-500/20">
                        <Key className="w-4 h-4 text-amber-400" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="text-text-primary font-medium">{keyItem.name}</span>
                          <Badge variant={keyItem.enabled ? 'success' : 'warning'}>
                            {keyItem.enabled
                              ? t('apiKeys.enabledBadge')
                              : t('apiKeys.disabledBadge')}
                          </Badge>
                        </div>
                        <div className="flex items-center gap-2 mt-1">
                          <code className="text-sm text-text-secondary font-mono truncate">
                            {visible ? keyItem.api_key : maskKey(keyItem.api_key)}
                          </code>
                          <button
                            onClick={() => toggleVisible(keyItem.id)}
                            className="text-text-muted hover:text-text-primary transition-colors"
                            title={visible ? t('apiKeys.hide') : t('apiKeys.show')}
                          >
                            {visible ? (
                              <EyeOff className="w-3.5 h-3.5" />
                            ) : (
                              <Eye className="w-3.5 h-3.5" />
                            )}
                          </button>
                          <button
                            onClick={() => copyKey(keyItem.id, keyItem.api_key)}
                            className="text-text-muted hover:text-text-primary transition-colors"
                            title={copied ? t('apiKeys.copied') : t('apiKeys.copyKey')}
                          >
                            <Copy className="w-3.5 h-3.5" />
                          </button>
                          {copied && (
                            <span className="text-xs text-emerald-500">
                              {t('apiKeys.copied')}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button
                        onClick={() => setEditingKey(keyItem)}
                        className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
                      >
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(keyItem.id)}
                        className="p-1.5 rounded-lg text-text-muted hover:text-red-600 dark:hover:text-red-300 hover:bg-red-500/10 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                </GlassCard>
              </div>
            )
          })}
        </div>
      ) : (
        <GlassCard className="p-8 text-center">
          <Key className="w-12 h-12 mx-auto mb-3 text-text-muted" />
          <p className="text-text-secondary">{t('apiKeys.noKeys')}</p>
          <p className="text-text-muted text-sm mt-1">{t('apiKeys.noKeysDesc')}</p>
        </GlassCard>
      )}

      {showCreate && <CreateKeyModal onClose={() => setShowCreate(false)} />}
      {editingKey && (
        <EditKeyModal keyItem={editingKey} onClose={() => setEditingKey(undefined)} />
      )}
      {deleteConfirmId && (
        <ConfirmDialog
          title={t('apiKeys.deleteConfirm')}
          message={t('apiKeys.deleteConfirm')}
          danger
          loading={deleteMut.isPending}
          onConfirm={() => {
            deleteMut.mutate(deleteConfirmId)
          }}
          onCancel={() => setDeleteConfirmId(null)}
        />
      )}
    </div>
  )
}

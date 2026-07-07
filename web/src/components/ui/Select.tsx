import {
  forwardRef,
  useState,
  useRef,
  useEffect,
  useLayoutEffect,
  useCallback,
  useMemo,
} from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown } from 'lucide-react'

interface SelectOption {
  value: string
  label: string
}

interface SelectProps {
  label?: string
  error?: string
  options: SelectOption[]
  value?: string
  defaultValue?: string
  onChange?: (e: React.ChangeEvent<HTMLSelectElement>) => void
  disabled?: boolean
  name?: string
  id?: string
  className?: string
  placeholder?: string
  searchable?: boolean
  size?: 'sm' | 'md'
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  (
    {
      label,
      error,
      options,
      value: controlledValue,
      defaultValue,
      onChange,
      disabled = false,
      name,
      id,
      className = '',
      placeholder,
      searchable,
      size = 'md',
    },
    ref,
  ) => {
    const { t } = useTranslation()
    const isControlled = controlledValue !== undefined
    const [internalValue, setInternalValue] = useState(
      defaultValue ?? (options[0]?.value ?? ''),
    )
    const currentValue = isControlled ? controlledValue : internalValue

    const [open, setOpen] = useState(false)
    const [highlightIndex, setHighlightIndex] = useState(-1)
    const [query, setQuery] = useState('')
    const [menuPlacement, setMenuPlacement] = useState<'down' | 'up'>('down')

    const containerRef = useRef<HTMLDivElement>(null)
    const triggerRef = useRef<HTMLButtonElement>(null)
    const menuRef = useRef<HTMLDivElement>(null)
    const nativeSelectRef = useRef<HTMLSelectElement | null>(null)
    const searchInputRef = useRef<HTMLInputElement>(null)
    const optionRefs = useRef<(HTMLDivElement | null)[]>([])

    const currentOption = options.find((o) => o.value === currentValue)
    const displayText = currentOption?.label ?? ''

    // Determine if search is enabled
    const isSearchable = searchable ?? options.length > 8

    // Filter options based on search query
    const visibleOptions = useMemo(() => {
      if (!isSearchable || !query) return options
      const q = query.toLowerCase()
      return options.filter((o) => o.label.toLowerCase().includes(q))
    }, [options, query, isSearchable])

    // Reset highlight when visible options change or menu opens
    useEffect(() => {
      if (open) {
        if (visibleOptions.length === 0) {
          setHighlightIndex(-1)
        } else {
          const idx = visibleOptions.findIndex((o) => o.value === currentValue)
          setHighlightIndex(idx >= 0 ? idx : 0)
        }
      }
    }, [open, visibleOptions, currentValue])

    // Reset option refs array when visible options change
    useEffect(() => {
      optionRefs.current = optionRefs.current.slice(0, visibleOptions.length)
    }, [visibleOptions])

    // Focus search input when opening
    useEffect(() => {
      if (open && isSearchable && searchInputRef.current) {
        requestAnimationFrame(() => {
          searchInputRef.current?.focus()
        })
      }
    }, [open, isSearchable])

    // Measure menu placement
    useLayoutEffect(() => {
      if (!open || !triggerRef.current || !menuRef.current) return
      const triggerRect = triggerRef.current.getBoundingClientRect()
      const menuHeight = menuRef.current.offsetHeight
      const spaceBelow = window.innerHeight - triggerRect.bottom - 8
      const spaceAbove = triggerRect.top - 8
      if (spaceBelow < menuHeight && spaceAbove > spaceBelow) {
        setMenuPlacement('up')
      } else {
        setMenuPlacement('down')
      }
    }, [open])

    // Scroll highlighted option into view
    useEffect(() => {
      if (open && highlightIndex >= 0) {
        const el = optionRefs.current[highlightIndex]
        if (el) {
          el.scrollIntoView({ block: 'nearest' })
        }
      }
    }, [open, highlightIndex])

    // Click outside to close (delayed one tick to avoid conflict with opening click)
    useEffect(() => {
      if (!open) return
      const handleClickOutside = (e: MouseEvent) => {
        if (
          containerRef.current &&
          !containerRef.current.contains(e.target as Node)
        ) {
          setOpen(false)
          setQuery('')
        }
      }
      const timer = setTimeout(() => {
        document.addEventListener('mousedown', handleClickOutside)
      }, 0)
      return () => {
        clearTimeout(timer)
        document.removeEventListener('mousedown', handleClickOutside)
      }
    }, [open])

    // Sync hidden select value
    useEffect(() => {
      if (nativeSelectRef.current) {
        nativeSelectRef.current.value = currentValue
      }
    }, [currentValue])

    const selectOption = useCallback(
      (val: string) => {
        if (!isControlled) {
          setInternalValue(val)
        }
        if (nativeSelectRef.current) {
          nativeSelectRef.current.value = val
          const fakeEvent = {
            target: nativeSelectRef.current,
            currentTarget: nativeSelectRef.current,
          } as React.ChangeEvent<HTMLSelectElement>
          onChange?.(fakeEvent)
        }
        setOpen(false)
        setQuery('')
        triggerRef.current?.focus()
      },
      [isControlled, onChange],
    )

    const handleTriggerClick = () => {
      if (disabled) return
      setOpen((prev) => !prev)
      if (!open) {
        setQuery('')
      }
    }

    const handleTriggerKeyDown = (e: React.KeyboardEvent) => {
      if (disabled) return
      switch (e.key) {
        case 'ArrowDown':
        case 'ArrowUp':
        case 'Enter':
        case ' ':
          e.preventDefault()
          setOpen(true)
          setQuery('')
          break
      }
    }

    const handleMenuKeyDown = (e: React.KeyboardEvent) => {
      if (disabled) return
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          setHighlightIndex((prev) =>
            prev < visibleOptions.length - 1 ? prev + 1 : 0,
          )
          break
        case 'ArrowUp':
          e.preventDefault()
          setHighlightIndex((prev) =>
            prev > 0 ? prev - 1 : visibleOptions.length - 1,
          )
          break
        case 'Home':
          e.preventDefault()
          setHighlightIndex(0)
          break
        case 'End':
          e.preventDefault()
          setHighlightIndex(visibleOptions.length - 1)
          break
        case 'Enter':
          e.preventDefault()
          if (highlightIndex >= 0 && highlightIndex < visibleOptions.length) {
            selectOption(visibleOptions[highlightIndex].value)
          }
          break
        case 'Escape':
          e.preventDefault()
          setOpen(false)
          setQuery('')
          triggerRef.current?.focus()
          break
        case 'Tab':
          setOpen(false)
          setQuery('')
          break
      }
    }

    const triggerSizeClasses =
      size === 'sm'
        ? 'rounded-lg px-2.5 py-1 text-sm'
        : 'rounded-xl px-4 py-2.5 text-sm'

    return (
      <div className="space-y-1.5">
        {label && (
          <label className="block text-sm font-medium text-text-secondary">
            {label}
          </label>
        )}
        <div ref={containerRef} className="relative">
          {/* Hidden native select for form compatibility and as change event source */}
          <select
            ref={(node) => {
              nativeSelectRef.current = node
              if (typeof ref === 'function') ref(node)
              else if (ref) (ref as React.MutableRefObject<HTMLSelectElement | null>).current = node
            }}
            name={name}
            id={id}
            value={currentValue}
            className="sr-only"
            tabIndex={-1}
            aria-hidden
          >
            {options.map((opt) => (
              <option key={opt.value} value={opt.value} />
            ))}
          </select>

          {/* Trigger button */}
          <button
            ref={triggerRef}
            type="button"
            role="combobox"
            aria-haspopup="listbox"
            aria-expanded={open}
            aria-controls={open ? 'select-listbox' : undefined}
            aria-activedescendant={
              open && highlightIndex >= 0
                ? `select-option-${visibleOptions[highlightIndex]?.value}`
                : undefined
            }
            disabled={disabled}
            onClick={handleTriggerClick}
            onKeyDown={handleTriggerKeyDown}
            className={`
              w-full flex items-center gap-2 border bg-surface-light text-text-primary
              transition-all duration-200
              focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent
              ${error ? 'border-red-500/50' : 'border-surface-border hover:border-accent/30'}
              ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}
              ${triggerSizeClasses}
              ${className}
            `}
          >
            <span className="flex-1 text-left truncate">
              {displayText || placeholder || ''}
            </span>
            <ChevronDown
              className={`w-4 h-4 text-text-muted shrink-0 transition-transform duration-200 ${
                open ? 'rotate-180' : ''
              }`}
            />
          </button>

          {/* Dropdown menu */}
          {open && (
            <div
              ref={menuRef}
              id="select-listbox"
              role="listbox"
              onKeyDown={handleMenuKeyDown}
              className={`
                absolute z-50 min-w-full bg-surface-light border border-card-border rounded-xl shadow-xl p-1.5 text-sm
                ${menuPlacement === 'down' ? 'top-full mt-1' : 'bottom-full mb-1'}
              `}
            >
              {/* Search input */}
              {isSearchable && (
                <div className="px-1 pb-1">
                  <input
                    ref={searchInputRef}
                    type="text"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={handleMenuKeyDown}
                    aria-label={t('common.searchPlaceholder')}
                    placeholder={t('common.searchPlaceholder')}
                    className="w-full px-2.5 py-1.5 rounded-lg bg-surface-light border border-surface-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent/50"
                  />
                </div>
              )}

              {/* Options */}
              <div className="max-h-64 overflow-auto">
                {visibleOptions.length === 0 ? (
                  <div className="px-3 py-2 text-sm text-text-muted text-center">
                    {t('common.noResults')}
                  </div>
                ) : (
                  visibleOptions.map((opt, idx) => (
                    <div
                      key={opt.value}
                      ref={(el) => {
                        optionRefs.current[idx] = el
                      }}
                      id={`select-option-${opt.value}`}
                      role="option"
                      aria-selected={opt.value === currentValue}
                      className={`
                        flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer transition-colors
                        ${
                          opt.value === currentValue
                            ? 'bg-accent/20 text-accent-light'
                            : highlightIndex === idx
                              ? 'bg-card text-text-primary'
                              : 'text-text-primary hover:bg-card'
                        }
                      `}
                      onClick={() => selectOption(opt.value)}
                      onMouseEnter={() => setHighlightIndex(idx)}
                    >
                      <span className="flex-1">{opt.label}</span>
                      {opt.value === currentValue && (
                        <Check className="w-3.5 h-3.5 text-accent-light shrink-0" />
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
          )}
        </div>
        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>
    )
  },
)

Select.displayName = 'Select'

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { messages, type Locale, type Messages } from './messages'

const STORAGE_KEY = 'ipv6-proxy-admin:lang'

type Vars = Record<string, string | number>

type Translate = (path: string, vars?: Vars) => string

interface I18nValue {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: Translate
  m: Messages
}

const I18nContext = createContext<I18nValue | null>(null)

function readStored(): Locale {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY)
    if (v === 'en' || v === 'zh') return v
  } catch {
    /* ignore */
  }
  return 'zh'
}

function lookup(dict: Messages, path: string): string | undefined {
  let cur: unknown = dict
  for (const part of path.split('.')) {
    if (typeof cur !== 'object' || cur === null || !(part in cur)) return undefined
    cur = (cur as Record<string, unknown>)[part]
  }
  return typeof cur === 'string' ? cur : undefined
}

function interpolate(template: string, vars?: Vars): string {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (_, key: string) =>
    vars[key] === undefined ? `{${key}}` : String(vars[key]),
  )
}

function applyDocumentLocale(locale: Locale, title: string) {
  document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'
  document.title = title
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    const initial = readStored()
    applyDocumentLocale(initial, messages[initial].app.title)
    return initial
  })

  const t = useCallback<Translate>((path, vars) => {
    const raw = lookup(messages[locale], path) ?? lookup(messages.zh, path) ?? path
    return interpolate(raw, vars)
  }, [locale])

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next)
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    applyDocumentLocale(locale, messages[locale].app.title)
  }, [locale])

  const value = useMemo<I18nValue>(
    () => ({ locale, setLocale, t, m: messages[locale] }),
    [locale, setLocale, t],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within LocaleProvider')
  return ctx
}

export function LanguageSwitcher({ className }: { className?: string }) {
  const { locale, setLocale, m } = useI18n()
  return (
    <div className={className ? `lang-switch ${className}` : 'lang-switch'} role="group" aria-label={locale === 'zh' ? '语言' : 'Language'}>
      <button type="button" className={locale === 'zh' ? 'active' : ''} onClick={() => setLocale('zh')}>
        {m.lang.zh}
      </button>
      <button type="button" className={locale === 'en' ? 'active' : ''} onClick={() => setLocale('en')}>
        {m.lang.en}
      </button>
    </div>
  )
}

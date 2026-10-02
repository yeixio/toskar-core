import { afterEach, describe, expect, it } from 'vitest'
import i18n, { applyLanguage, availableLanguages } from '@/i18n'
import { pseudoLocales } from '@/i18n/languages'
import type { AutomationNotification, AutomationSchedule } from '@/types/api'
import { civilToISO, composePrompt, notificationLabel, parseAutomationRequest, visibleTask } from './parseRequest'
import { requestWordsFor } from './requestWords'
import { readNumber } from './requestWords/match'

const zone = 'America/Los_Angeles'
// Monday, September 28, 2026, 8:00 AM in Los Angeles.
const morning = new Date('2026-09-28T15:00:00Z')
const tomorrowAt9 = '2026-09-29T16:00:00.000Z'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

describe('parseAutomationRequest', () => {
  it('turns a morning price check into a daily threshold task', () => {
    const parsed = parseAutomationRequest(
      'Every morning at 8:00 AM, check this product and tell me if the price is below $500.',
      morning,
      zone,
    )
    expect(parsed.name).toBe('Price below $500')
    expect(parsed.schedule).toMatchObject({ kind: 'daily', time_zone: zone, hour: 8, minute: 0 })
    expect(parsed.notification).toEqual({
      mode: 'condition',
      condition: { kind: 'threshold', op: 'below', value: 500, currency: 'USD' },
    })
    expect(parsed.prompt).toContain('{"price": 420}')
    expect(parsed.prompt).toContain('numeric price in USD')
    expect(visibleTask(parsed.prompt)).toBe('Check this product. Report the current price.')
    expect(parsed.notes).toEqual([])
  })

  it('turns a stock check into an interval that notifies when available', () => {
    const parsed = parseAutomationRequest(
      'Every six hours, check whether this item is back in stock. Notify me only when it becomes available.',
      morning,
      zone,
    )
    expect(parsed.name).toBe('Stock check')
    expect(parsed.schedule).toMatchObject({ kind: 'interval', every_seconds: 6 * 3600 })
    expect(parsed.notification.mode).toBe('condition')
    expect(parsed.notification.condition).toEqual({ kind: 'available' })
    expect(parsed.prompt).toContain('{"available": true}')
  })

  it('turns a Friday release check into a weekly task and notes the default time', () => {
    const parsed = parseAutomationRequest(
      'Every Friday, check for new releases of this software and summarize what changed.',
      morning,
      zone,
    )
    expect(parsed.name).toBe('Release check')
    expect(parsed.schedule).toMatchObject({ kind: 'weekly', weekday: 5, hour: 8, minute: 0 })
    expect(parsed.notification.mode).toBe('always')
    expect(parsed.prompt).not.toContain('{"price"')
    expect(parsed.notes).toEqual(['No time was given, so this runs at 8:00 AM.'])
  })

  it('schedules a one-time research prompt for tomorrow morning in the task time zone', () => {
    const parsed = parseAutomationRequest('Run this research prompt once tomorrow at 9:00 AM.', morning, zone)
    expect(parsed.name).toBe('Research')
    expect(parsed.schedule.kind).toBe('once')
    expect(parsed.schedule.at).toBe(tomorrowAt9)
    expect(parsed.notification.mode).toBe('always')
  })

  it('keeps store-only and change requests out of the price condition', () => {
    const quiet = parseAutomationRequest("Don't notify me. Every day at 7:00 AM, check the news.", morning, zone)
    expect(quiet.notification.mode).toBe('none')
    expect(quiet.schedule).toMatchObject({ kind: 'daily', hour: 7, minute: 0 })

    const change = parseAutomationRequest('Every day at 7:00 AM, notify me only when the page changes.', morning, zone)
    expect(change.notification.mode).toBe('change')
  })

  it('reads 24-hour times, evening hours, hourly checks, and other currencies in English', () => {
    expect(parseAutomationRequest('Every day at 18:30, check the news.', morning, zone).schedule).toMatchObject({ hour: 18, minute: 30 })
    expect(parseAutomationRequest('Every evening at 6:30, summarize the news.', morning, zone).schedule).toMatchObject({ hour: 18, minute: 30 })
    expect(parseAutomationRequest('Every hour, check the queue.', morning, zone).schedule).toMatchObject({ kind: 'interval', every_seconds: 3600 })
    expect(parseAutomationRequest('Every day, tell me if the price is above 1,299.99 EUR.', morning, zone).notification).toEqual({
      mode: 'condition',
      condition: { kind: 'threshold', op: 'above', value: 1299.99, currency: 'EUR' },
    })
  })

  it('reads English in every App language, with an amount in that language’s currency', () => {
    const parsed = parseAutomationRequest('Every morning at 8:00 AM, tell me if the price is below 500.', morning, zone, 'de')
    expect(parsed.schedule).toMatchObject({ kind: 'daily', hour: 8, minute: 0 })
    expect(parsed.notification.condition).toEqual({ kind: 'threshold', op: 'below', value: 500, currency: 'EUR' })
    expect(visibleTask(parsed.prompt)).toBe('Tell me if the price is below 500.')
  })

  it('keeps a leading clause that says what to do as well as when', () => {
    const parsed = parseAutomationRequest('Täglich um 7:30 Uhr prüfen, ob der Preis unter 19,99 € fällt.', morning, zone, 'de')
    expect(parsed.schedule).toMatchObject({ kind: 'daily', hour: 7, minute: 30 })
    expect(parsed.notification.condition).toEqual({ kind: 'threshold', op: 'below', value: 19.99, currency: 'EUR' })
    expect(visibleTask(parsed.prompt)).toBe('Täglich um 7:30 Uhr prüfen, ob der Preis unter 19,99 € fällt.')
  })

  it('asks for the time when it cannot tell, including a language the App is not in', () => {
    expect(() => parseAutomationRequest('Check the news.', morning, zone)).toThrow('Describe when it should run')
    expect(() => parseAutomationRequest('Jeden Morgen um 8 Uhr die Nachrichten prüfen.', morning, zone, 'en')).toThrow()
  })

  it('converts a civil time in Pacific time to the matching UTC instant', () => {
    expect(civilToISO('2026-01-15T09:00', zone)).toBe('2026-01-15T17:00:00.000Z')
  })
})

describe('prices', () => {
  it('reads numbers as people write them', () => {
    expect(readNumber('1,299.99')).toBe(1299.99)
    expect(readNumber('1.299,99')).toBe(1299.99)
    expect(readNumber('1 299,99')).toBe(1299.99)
    expect(readNumber('2.500')).toBe(2500)
    expect(readNumber('4.99')).toBe(4.99)
    expect(readNumber('19,5')).toBe(19.5)
    expect(readNumber('about 5')).toBeNull()
  })

  it('asks the run for the price in the threshold’s currency, and shows the task without it', () => {
    const notification: AutomationNotification = { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'EUR' } }
    const prompt = composePrompt('Prüfe dieses Produkt.', notification)
    expect(prompt).toContain('numeric price in EUR')
    expect(visibleTask(prompt)).toBe('Prüfe dieses Produkt.')
    // A currency change replaces the instruction rather than adding a second one.
    const yen = composePrompt(prompt, { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'JPY' } })
    expect(yen.match(/numeric price/g)).toHaveLength(1)
    expect(yen).toContain('in JPY')
  })

  it('shows a threshold in its currency, and an older one without a currency in dollars', async () => {
    expect(notificationLabel({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500 } })).toBe('Notify when the price is below $500')
    await applyLanguage('de')
    expect(notificationLabel({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'EUR' } })).toMatch(/500\s€/)
  })
})

interface Case {
  text: string
  schedule: Partial<AutomationSchedule>
  notification?: AutomationNotification
}

interface LanguageCases {
  /** The threshold in the language's example request (form.describePlaceholder). */
  example: { value: number; currency: string }
  /** What the example's task becomes, without the schedule and the condition. */
  task?: string
  cases: Case[]
}

const available: AutomationNotification = { mode: 'condition', condition: { kind: 'available' } }
const change: AutomationNotification = { mode: 'change' }
const none: AutomationNotification = { mode: 'none' }
const threshold = (op: 'below' | 'above', value: number, currency: string): AutomationNotification => ({
  mode: 'condition',
  condition: { kind: 'threshold', op, value, currency },
})

const languages: Record<string, LanguageCases> = {
  de: {
    example: { value: 500, currency: 'EUR' },
    task: 'Prüfe dieses Produkt. Nenne den aktuellen Preis.',
    cases: [
      { text: 'Jeden Freitag um 18:30 die Release-Notes zusammenfassen.', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: 'Freitags um 7 Uhr den Wetterbericht holen.', schedule: { kind: 'weekly', weekday: 5, hour: 7, minute: 0 } },
      { text: 'Alle zwei Stunden prüfen, ob der Artikel wieder auf Lager ist.', schedule: { kind: 'interval', every_seconds: 7200 }, notification: available },
      { text: 'Stündlich die Warteschlange prüfen.', schedule: { kind: 'interval', every_seconds: 3600 } },
      { text: 'Morgen um 9 Uhr einmal diese Recherche ausführen.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: 'Heute Morgen um 11 Uhr die Bestellung prüfen.', schedule: { kind: 'once', at: '2026-09-28T18:00:00.000Z' } },
      { text: 'Jeden Abend um 8 die Nachrichten zusammenfassen, nur bei Änderungen.', schedule: { kind: 'daily', hour: 20, minute: 0 }, notification: change },
      { text: 'Täglich um 18 Uhr den Bericht speichern, nicht benachrichtigen.', schedule: { kind: 'daily', hour: 18, minute: 0 }, notification: none },
      { text: 'Jeden Tag Bescheid geben, wenn der Preis über 1.299,99 € steigt.', schedule: { kind: 'daily', hour: 8, minute: 0 }, notification: threshold('above', 1299.99, 'EUR') },
    ],
  },
  es: {
    example: { value: 500, currency: 'EUR' },
    task: 'Revisa este producto. Indica el precio actual.',
    cases: [
      { text: 'Todos los lunes a las 18:30, revisa las novedades.', schedule: { kind: 'weekly', weekday: 1, hour: 18, minute: 30 } },
      { text: 'Cada 30 minutos, comprueba si vuelve a estar disponible.', schedule: { kind: 'interval', every_seconds: 1800 }, notification: available },
      { text: 'Mañana a las 9, ejecuta esta investigación una vez.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: 'Hoy por la mañana a las 11, revisa el pedido.', schedule: { kind: 'once', at: '2026-09-28T18:00:00.000Z' } },
      { text: 'Todas las noches a las 10, resume las noticias y avísame solo si hay cambios.', schedule: { kind: 'daily', hour: 22, minute: 0 }, notification: change },
      { text: 'Cada día a las 8 y media, avísame si el precio supera los 80 dólares.', schedule: { kind: 'daily', hour: 8, minute: 30 }, notification: threshold('above', 80, 'USD') },
    ],
  },
  fr: {
    example: { value: 500, currency: 'EUR' },
    task: 'Vérifie ce produit. Indique le prix actuel.',
    cases: [
      { text: 'Tous les vendredis à 18 h 30, résume les nouveautés.', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: 'Toutes les 2 heures, vérifie si l’article est de nouveau en stock.', schedule: { kind: 'interval', every_seconds: 7200 }, notification: available },
      { text: 'Demain à 9 h, lance cette recherche une fois.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: 'Chaque soir à 8 heures, préviens-moi si le prix dépasse 1 299,99 €.', schedule: { kind: 'daily', hour: 20, minute: 0 }, notification: threshold('above', 1299.99, 'EUR') },
      { text: 'Tous les jours à 18h, résume les actualités et préviens-moi quand ça change.', schedule: { kind: 'daily', hour: 18, minute: 0 }, notification: change },
    ],
  },
  it: {
    example: { value: 500, currency: 'EUR' },
    task: 'Controlla questo prodotto. Indica il prezzo attuale.',
    cases: [
      { text: 'Ogni venerdì alle 18:30, riassumi le novità.', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: 'Ogni 15 minuti controlla se è di nuovo disponibile.', schedule: { kind: 'interval', every_seconds: 900 }, notification: available },
      { text: 'Domani alle 9 esegui questa ricerca una volta.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: 'Ogni sera alle 8 riassumi le notizie e avvisami solo quando cambia.', schedule: { kind: 'daily', hour: 20, minute: 0 }, notification: change },
    ],
  },
  'pt-BR': {
    example: { value: 2500, currency: 'BRL' },
    task: 'Verifique este produto. Informe o preço atual.',
    cases: [
      { text: 'Toda sexta-feira às 18:30, resuma as novidades.', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: 'A cada 6 horas, veja se o item está em estoque.', schedule: { kind: 'interval', every_seconds: 6 * 3600 }, notification: available },
      { text: 'Amanhã às 9, execute esta pesquisa uma vez.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: 'Todo dia às 8 da noite, me avise só quando mudar.', schedule: { kind: 'daily', hour: 20, minute: 0 }, notification: change },
      { text: 'Todos os dias às 7h, me avise se o preço passar de 2 mil reais.', schedule: { kind: 'daily', hour: 7, minute: 0 }, notification: threshold('above', 2000, 'BRL') },
    ],
  },
  ja: {
    example: { value: 50000, currency: 'JPY' },
    task: 'この商品をチェックして、価格が5万円を下回ったら教えてください。',
    cases: [
      { text: '毎週金曜日の18時30分に、リリース情報をまとめて。', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: '2時間ごとに在庫を確認して、再入荷したら通知して。', schedule: { kind: 'interval', every_seconds: 7200 }, notification: available },
      { text: '明日の午後3時に一度だけ実行して。', schedule: { kind: 'once', at: '2026-09-29T22:00:00.000Z' } },
      { text: '毎日18時に確認して、変わったら通知して。', schedule: { kind: 'daily', hour: 18, minute: 0 }, notification: change },
      { text: '毎晩8時半にニュースをまとめて。', schedule: { kind: 'daily', hour: 20, minute: 30 } },
      { text: '毎日、価格が1000ドル以上になったら教えて。', schedule: { kind: 'daily', hour: 8, minute: 0 }, notification: threshold('above', 1000, 'USD') },
    ],
  },
  ko: {
    example: { value: 500000, currency: 'KRW' },
    task: '이 상품을 확인하고 가격이 50만 원 아래로 내려가면 알려 주세요.',
    cases: [
      { text: '매주 금요일 오후 6시 30분에 새 버전을 확인해 줘.', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: '3시간마다 재고를 확인하고 재입고되면 알려 줘.', schedule: { kind: 'interval', every_seconds: 3 * 3600 }, notification: available },
      { text: '내일 오전 9시에 한 번 실행해 줘.', schedule: { kind: 'once', at: tomorrowAt9 } },
      { text: '매일 저녁 7시에 뉴스를 요약하고 바뀌면 알려 줘.', schedule: { kind: 'daily', hour: 19, minute: 0 }, notification: change },
      { text: '매일 18시에 확인하고 가격이 10만 원 이상이면 알려 줘.', schedule: { kind: 'daily', hour: 18, minute: 0 }, notification: threshold('above', 100000, 'KRW') },
    ],
  },
  'zh-Hans': {
    example: { value: 3000, currency: 'CNY' },
    task: '检查这个商品，如果价格低于3000元就告诉我。',
    cases: [
      { text: '每周五晚上6点半，总结本周的新版本。', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: '每两小时检查一次库存，有货时通知我。', schedule: { kind: 'interval', every_seconds: 7200 }, notification: available },
      { text: '明天下午3点运行一次。', schedule: { kind: 'once', at: '2026-09-29T22:00:00.000Z' } },
      { text: '每天晚上八点，有变化时通知我。', schedule: { kind: 'daily', hour: 20, minute: 0 }, notification: change },
      { text: '每天18:30检查，价格高于¥1万时告诉我。', schedule: { kind: 'daily', hour: 18, minute: 30 }, notification: threshold('above', 10000, 'CNY') },
    ],
  },
  'zh-Hant': {
    example: { value: 3000, currency: 'TWD' },
    task: '檢查這個商品，如果價格低於3000元就告訴我。',
    cases: [
      { text: '每週五晚上6點半，總結本週的新版本。', schedule: { kind: 'weekly', weekday: 5, hour: 18, minute: 30 } },
      { text: '每三個小時檢查庫存，補貨時通知我。', schedule: { kind: 'interval', every_seconds: 3 * 3600 }, notification: available },
      { text: '每天晚上9點，價格高於1萬元時通知我。', schedule: { kind: 'daily', hour: 21, minute: 0 }, notification: threshold('above', 10000, 'TWD') },
    ],
  },
}

describe('requests in the App language', () => {
  it('cover every language the App can be shown in', () => {
    const shown = availableLanguages.filter((language) => !pseudoLocales.includes(language))
    expect(shown.filter((language) => !requestWordsFor(language))).toEqual([])
    expect(Object.keys(languages).sort()).toEqual(shown.filter((language) => language !== 'en').sort())
  })

  for (const [language, { example, task, cases }] of Object.entries(languages)) {
    describe(language, () => {
      it('reads the example request in the box', async () => {
        await applyLanguage(language)
        const placeholder = i18n.t('automations:form.describePlaceholder')
        expect(placeholder).not.toMatch(/every morning/i)
        const parsed = parseAutomationRequest(placeholder, morning, zone)
        expect(parsed.schedule).toMatchObject({ kind: 'daily', hour: 8, minute: 0 })
        expect(parsed.notification).toEqual(threshold('below', example.value, example.currency))
        expect(parsed.notes).toEqual([])
        if (task) expect(visibleTask(parsed.prompt)).toBe(task)
        expect(i18n.t('automations:parse.describeWhen')).not.toContain('every morning')
      })

      for (const { text, schedule, notification } of cases) {
        it(`reads “${text}”`, async () => {
          await applyLanguage(language)
          const parsed = parseAutomationRequest(text, morning, zone)
          expect(parsed.schedule).toMatchObject(schedule)
          expect(parsed.notification).toEqual(notification ?? { mode: 'always' })
        })
      }

      it('still reads English', async () => {
        await applyLanguage(language)
        const parsed = parseAutomationRequest('Every Friday at 6:30 PM, check for new releases.', morning, zone)
        expect(parsed.schedule).toMatchObject({ kind: 'weekly', weekday: 5, hour: 18, minute: 30 })
      })
    })
  }
})

/**
 * A sentence in each language read aloud, to preview a voice in its own
 * language (#1304). Not UI text: each is in the language it previews.
 */
const samples: Record<string, string> = {
  ar: 'مرحبًا، أنا مساعدك. كيف يمكنني المساعدة اليوم؟',
  cs: 'Dobrý den, jsem váš asistent. Jak vám dnes mohu pomoci?',
  da: 'Hej, jeg er din assistent. Hvordan kan jeg hjælpe i dag?',
  de: 'Hallo, ich bin dein Assistent. Wie kann ich heute helfen?',
  el: 'Γεια σας, είμαι ο βοηθός σας. Πώς μπορώ να βοηθήσω σήμερα;',
  en: "Hi there, I'm your assistant. How can I help today?",
  es: 'Hola, soy tu asistente. ¿En qué puedo ayudarte hoy?',
  fa: 'سلام، من دستیار شما هستم. امروز چطور می‌توانم کمک کنم؟',
  fi: 'Hei, olen avustajasi. Miten voin auttaa tänään?',
  fr: "Bonjour, je suis votre assistant. Comment puis-je vous aider aujourd'hui ?",
  hi: 'नमस्ते, मैं आपका सहायक हूँ। आज मैं कैसे मदद कर सकता हूँ?',
  hu: 'Szia, én vagyok az asszisztensed. Miben segíthetek ma?',
  id: 'Halo, saya asisten Anda. Apa yang bisa saya bantu hari ini?',
  it: 'Ciao, sono il tuo assistente. Come posso aiutarti oggi?',
  ja: 'こんにちは、アシスタントです。今日は何をお手伝いしましょうか？',
  ko: '안녕하세요, 저는 당신의 어시스턴트입니다. 오늘 무엇을 도와드릴까요?',
  nb: 'Hei, jeg er assistenten din. Hvordan kan jeg hjelpe i dag?',
  nl: 'Hallo, ik ben je assistent. Waarmee kan ik je vandaag helpen?',
  no: 'Hei, jeg er assistenten din. Hvordan kan jeg hjelpe i dag?',
  pl: 'Cześć, jestem twoim asystentem. W czym mogę dziś pomóc?',
  pt: 'Olá, sou o seu assistente. Como posso ajudar hoje?',
  ru: 'Здравствуйте, я ваш помощник. Чем я могу помочь сегодня?',
  sv: 'Hej, jag är din assistent. Hur kan jag hjälpa till i dag?',
  sw: 'Habari, mimi ni msaidizi wako. Nikusaidie nini leo?',
  tr: 'Merhaba, ben asistanınım. Bugün nasıl yardımcı olabilirim?',
  uk: 'Привіт, я ваш помічник. Чим я можу допомогти сьогодні?',
  vi: 'Xin chào, tôi là trợ lý của bạn. Hôm nay tôi có thể giúp gì?',
  zh: '你好，我是你的助手。今天我能帮你做什么？',
}

/** The preview sentence for lang, or English's. */
export function voiceSample(lang: string): string {
  return samples[lang] ?? samples.en
}

/** The credit Kitten TTS 2's voices ask for, shown wherever one is used; a name, not translated. */
export const KITTEN_CREDIT = 'Powered by KittenML'

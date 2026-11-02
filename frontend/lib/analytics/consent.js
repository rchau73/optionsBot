// The analytics consent (#577, docs/product/analytics/surveys.md §6, event
// plan §5): the one-time notice's words and the Account › Privacy row's. Kept
// in this one module so the person's final wording and counsel's answers (C1,
// C6) are a one-file edit. Each text is in EN and PT-BR; the monitor shows EN
// (it has no language setting yet), __tests__/consent-notice.test.jsx keeps the
// two equal and the draft sentences equal to surveys.md.

export const CONSENT_COPY = {
  en: {
    title: "Help us improve the monitor",
    // surveys.md §6, the draft consent text, sentence by sentence.
    draft: [
      "Your answers help us improve the monitor.",
      "We keep the score and the reason you choose, never amounts or positions.",
      "Any text you write is kept with your account and deleted with it.",
      "You can change your mind at any time in the account menu.",
    ],
    // What a yes records (event plan §5).
    kept: "With a yes we also record which screens and help you use, never amounts or positions.",
    accountLink: "Change it in Account",
    yes: "Yes",
    no: "No",
    saving: "Saving…",
    notSaved: "Not saved",
    retry: "Try again",
    rowLabel: "Usage analytics and surveys",
    status: { yes: "Yes", no: "No", unasked: "Not asked yet", unknown: "unknown" },
    rowHelp: "Records which screens and help you use, never amounts or positions. Surveys appear only with a yes.",
    deleteHint: "To delete them, use Delete my account in the account menu.",
  },
  pt: {
    title: "Ajude-nos a melhorar o monitor",
    draft: [
      "Suas respostas nos ajudam a melhorar o monitor.",
      "Guardamos a nota e o motivo escolhido, nunca valores ou posições.",
      "Um texto que você escrever fica junto da sua conta e é apagado com ela.",
      "Você pode mudar de ideia a qualquer momento no menu da conta.",
    ],
    kept: "Com um sim também registramos quais telas e ajudas você usa, nunca valores ou posições.",
    accountLink: "Mudar em Conta",
    yes: "Sim",
    no: "Não",
    saving: "Salvando…",
    notSaved: "Não salvo",
    retry: "Tentar de novo",
    rowLabel: "Análise de uso e pesquisas",
    status: { yes: "Sim", no: "Não", unasked: "Ainda não perguntado", unknown: "desconhecido" },
    rowHelp: "Registra quais telas e ajudas você usa, nunca valores ou posições. As pesquisas aparecem só com um sim.",
    deleteHint: "Para apagá-los, use Apagar minha conta no menu da conta.",
  },
};

/** The words the monitor shows today. */
export const COPY = CONSENT_COPY.en;

/** The pages where the notice is never shown: signed out, or setting a password. */
const NO_NOTICE = [/^\/login(\/|$)/, /^\/reset(\/|$)/];

/** Whether the notice may show: the person never answered (null) and the page is not sign-in or reset. */
export function noticeDue(consent, pathname) {
  return consent === null && !NO_NOTICE.some((re) => re.test(pathname ?? ""));
}

// The answers stored in this tab, told to every listener (hooks/useConsent.js),
// so the notice and the Account row show one answer: one given in either
// closes the notice and moves the switch at once.
const listeners = new Set();

/** Calls listener(consent) after each stored answer; → unsubscribe. */
export function onConsentSaved(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Stores the person's answer and sets the sender to it (#577): a no stops
 * tracking at once, before the request; a yes starts it only once the server
 * stored it, so a failed save sends nothing. Then the consent_answered event
 * (and consent_notice_shown, from the notice) — only a yes can send them —
 * and every onConsentSaved listener hears the stored answer.
 * deps: { send(consent) → { consent, note }, setConsent(consent), track(event, props) }.
 * → the server's answer; throws with its reason.
 */
export async function saveConsent(consent, { fromNotice = false } = {}, deps) {
  const yes = consent === true;
  if (!yes) deps.setConsent(false);
  const answer = await deps.send(yes);
  if (yes) {
    deps.setConsent(true);
    if (fromNotice) deps.track("consent_notice_shown");
    deps.track("consent_answered", { consent: "yes" });
  }
  for (const listener of [...listeners]) listener(yes);
  return answer;
}

/** The Account row's status word for a stored answer: true, false, null (not asked) or undefined (not read). */
export function consentStatus(consent, copy = COPY) {
  if (consent === true) return copy.status.yes;
  if (consent === false) return copy.status.no;
  if (consent === null) return copy.status.unasked;
  return copy.status.unknown;
}

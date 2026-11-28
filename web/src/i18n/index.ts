import { useSyncExternalStore } from "react";
import { en, type Messages } from "./en";

export type { Messages };
import { ru } from "./ru";

// Languages of the interface. A new one is a dictionary file typed as Messages and one line here.
// Only the interface is translated: builder replies, brief fields and stage names come from the
// builder package in the core and stay in its language.
export const LANGS = {
  en: { name: "English", locale: "en-US", messages: en },
  ru: { name: "Русский", locale: "ru-RU", messages: ru },
} satisfies Record<string, { name: string; locale: string; messages: Messages }>;

export type Lang = keyof typeof LANGS;

const KEY = "charter.lang";
export const DEFAULT_LANG: Lang = "en";

function read(): Lang {
  try {
    const v = localStorage.getItem(KEY);
    if (v && v in LANGS) return v as Lang;
  } catch {
    // no storage: the default language
  }
  return DEFAULT_LANG;
}

let lang = read();
const listeners = new Set<() => void>();
const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export const getLang = () => lang;

// onLang lets code outside React follow a switch.
export const onLang = subscribe;

export function setLang(next: Lang) {
  lang = next;
  document.documentElement.lang = next;
  try {
    localStorage.setItem(KEY, next);
  } catch {
    // the language still works for this visit
  }
  listeners.forEach((l) => l());
}

// t is the dictionary for code outside React (the store); components use useT so they redraw on a switch.
export const t = (): Messages => LANGS[lang].messages;

export function useT(): Messages {
  return LANGS[useSyncExternalStore(subscribe, getLang)].messages;
}

export function useLang(): Lang {
  return useSyncExternalStore(subscribe, getLang);
}

export const locale = () => LANGS[lang].locale;

export const formatTime = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" });

// plural picks a form by the language's rules: one/few/many in Russian, one/other in English.
export function plural(n: number, forms: Partial<Record<Intl.LDMLPluralRule, string>> & { other: string }) {
  return forms[new Intl.PluralRules(locale()).select(n)] ?? forms.other;
}

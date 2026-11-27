import { useSyncExternalStore } from "react";

// Lifecycle stages of an agent (STD-0002). The builder fills the request first.
export const STAGES = ["Заявка", "Описание", "Проверка", "Тень", "Работа"] as const;

export type BriefField = "goal" | "trigger" | "duties" | "escalation";

export const BRIEF_LABELS: Record<BriefField, string> = {
  goal: "Зачем",
  trigger: "Когда срабатывает",
  duties: "Что делает и чего не делает",
  escalation: "Кого звать",
};

export type Draft = {
  name: string;
  stage: number;
  brief: Partial<Record<BriefField, string>>;
  events: string[];
};

let state: Draft = { name: "Новый агент", stage: 0, brief: {}, events: [] };
const listeners = new Set<() => void>();

export const draft = {
  get: () => state,
  set(patch: Partial<Draft> | ((d: Draft) => Partial<Draft>)) {
    state = { ...state, ...(typeof patch === "function" ? patch(state) : patch) };
    listeners.forEach((l) => l());
  },
  subscribe(l: () => void) {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};

export function useDraft() {
  return useSyncExternalStore(draft.subscribe, draft.get);
}

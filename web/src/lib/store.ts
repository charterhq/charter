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

// The question an empty field asks: the canvas shows it instead of a blank.
export const BRIEF_QUESTIONS: Record<BriefField, string> = {
  goal: "Что должен делать агент и что будет, если его не делать?",
  trigger: "На что он реагирует – что запускает его работу?",
  duties: "Что он делает – и чего не делает никогда?",
  escalation: "Кого звать, если проблема подтвердилась?",
};

// Artifacts each stage produces; the canvas shows them under their stage.
export const STAGE_ARTIFACTS: string[][] = [
  ["Бриф"],
  ["Пакет агента", "Путь агента"],
  ["Набор проверки"],
  ["Отчет тени"],
  ["Карточка в Confluence"],
];

// A room is one conversation with the builder. The memory is the brief, not the room:
// every room sees the whole brief and what it lacks, so rooms stay short and their order does not matter.
export type Room = {
  id: string;
  title: string;
  // `field` on a builder message: the brief field that message filed the answer into.
  messages: { role: "user" | "assistant"; text: string; field?: BriefField }[];
  // The brief field the builder asked about last in this room: the next answer goes there.
  asking?: BriefField;
};

export type Draft = {
  name: string;
  rooms: Room[];
  room: string;
  stage: number;
  brief: Partial<Record<BriefField, string>>;
  events: string[];
  // The field the builder filled last: the canvas highlights it for a moment.
  lastFilled?: BriefField;
  // Where each field's value came from: a room, or "canvas" when the person edited it by hand.
  sources: Partial<Record<BriefField, { room: string; at: number }>>;
  // A request to show a field on the canvas; `n` grows so the same field can be asked twice.
  focus?: { field: BriefField; n: number };
  // Below lg the chat and the agent are tabs: which one is open.
  pane: "chat" | "agent";
};

export const CANVAS = "canvas";

let state: Draft = { name: "Новый агент", rooms: [], room: "", stage: 0, brief: {}, events: [], sources: {}, pane: "chat" };
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

export const FIELDS = Object.keys(BRIEF_LABELS) as BriefField[];

export function missingFields(d: Draft): BriefField[] {
  return FIELDS.filter((f) => !d.brief[f]);
}

// Chat → canvas: open the brief and point at the field.
export function focusField(field: BriefField) {
  draft.set((d) => ({ focus: { field, n: (d.focus?.n ?? 0) + 1 }, pane: "agent" }));
}

// Canvas → chat: open the room a value came from.
export function showRoom(id: string) {
  if (draft.get().rooms.some((r) => r.id === id)) draft.set({ room: id, pane: "chat" });
}

export function fillField(field: BriefField, value: string, room: string) {
  draft.set((d) => ({
    brief: { ...d.brief, [field]: value },
    sources: { ...d.sources, [field]: { room, at: Date.now() } },
    lastFilled: field,
  }));
}

export function updateRoom(id: string, patch: Partial<Room>) {
  draft.set((d) => ({ rooms: d.rooms.map((r) => (r.id === id ? { ...r, ...patch } : r)) }));
}

// A new room opens with what the brief still lacks, so the person can pick up anywhere.
export function openRoom(): Room {
  const d = draft.get();
  const missing = missingFields(d);
  const asking = missing[0];
  const text = !Object.keys(d.brief).length
    ? "Привет! Расскажи своими словами, что должен делать агент. Можно кидать ссылки, примеры и картинки – я разложу все по шагам и спрошу по одному."
    : missing.length
      ? `Продолжаем ${d.name}. В брифе не хватает: ${missing.map((f) => BRIEF_LABELS[f].toLowerCase()).join(", ")}. ${BRIEF_QUESTIONS[asking]} Или расскажи новую мысль – разложу ее в бриф.`
      : `Бриф ${d.name} собран. Расскажи, что добавить или поменять – поправлю нужное поле.`;
  const room: Room = { id: crypto.randomUUID(), title: "Новая беседа", messages: [{ role: "assistant", text }], asking };
  draft.set((s) => ({ rooms: [room, ...s.rooms], room: room.id }));
  return room;
}

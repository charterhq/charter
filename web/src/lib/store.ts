import { useSyncExternalStore } from "react";
import {
  ApiError,
  BUILDER,
  api,
  subscribe,
  token,
  type Conversation,
  type Field,
  type FieldSource,
  type Member,
  type Message,
  type ObjectEvent,
  type ObjectSummary,
  type ObjectView,
  type Stage,
} from "./api";

// The draft agent is a business object of the builder agent in the core; the brief is its state.
// The store mirrors the object: a snapshot from GET /v1/objects/{id}, then its event stream.
// The UI decides nothing: what is missing, where a value came from, the stages – all come from the core.

// Stages of STD-0002, shown until the builder package names its own.
const DEFAULT_STAGES: Stage[] = [
  { id: "request", title: "Заявка", artifacts: [{ kind: "brief", title: "Бриф" }] },
  { id: "description", title: "Описание", artifacts: [{ kind: "package", title: "Пакет агента" }, { kind: "path", title: "Путь агента" }] },
  { id: "check", title: "Проверка", artifacts: [{ kind: "eval", title: "Набор проверки" }] },
  { id: "shadow", title: "Тень", artifacts: [{ kind: "shadow", title: "Отчет тени" }] },
  { id: "work", title: "Работа", artifacts: [{ kind: "card", title: "Карточка в Confluence" }] },
];

export type BriefField = string;

// A room is one conversation with the builder. The memory is the brief, not the room: every room
// sees the whole brief and what it lacks, so rooms stay short and their order does not matter.
export type Room = {
  id: string;
  title: string;
  messages: Message[];
  loaded: boolean;
  // A run answering this room is under way: the chat shows "typing".
  running: boolean;
};

export type Draft = {
  status: "loading" | "ready" | "token" | "error";
  error?: string;
  // A short notice: an edit that lost to a newer one, a stream that dropped.
  notice?: string;
  me?: string;
  id: string;
  name: string;
  attrs: Record<string, unknown>;
  members: Member[];
  fields: Field[];
  schema?: Record<string, any>;
  brief: Record<string, unknown>;
  sources: Record<string, FieldSource>;
  missing: string[];
  rev: number;
  stages: Stage[];
  stage: number;
  rooms: Room[];
  room: string;
  live: boolean;
  // The field the builder filled last: the canvas highlights it for a moment.
  lastFilled?: BriefField;
  // A request to show a field on the canvas; `n` grows so the same field can be asked twice.
  focus?: { field: BriefField; n: number };
  // Below lg the chat and the agent are tabs: which one is open.
  pane: "chat" | "agent";
};

export const CANVAS = "canvas";

const empty: Draft = {
  status: "loading",
  id: "",
  name: "",
  attrs: {},
  members: [],
  fields: [],
  brief: {},
  sources: {},
  missing: [],
  rev: 0,
  stages: DEFAULT_STAGES,
  stage: 0,
  rooms: [],
  room: "",
  live: false,
  pane: "chat",
};

let state: Draft = empty;
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

// Fields that count toward "N of M": the ones the schema requires.
export function counted(d: Draft): Field[] {
  return d.fields.filter((f) => f.required || (f.min_items ?? 0) > 0);
}

export const label = (d: Draft, f: BriefField) => d.fields.find((x) => x.name === f)?.title || f;
export const question = (d: Draft, f: BriefField) => d.fields.find((x) => x.name === f)?.question ?? "";
// "cases/c3" → "cases": a message names paths, a card is a field.
export const fieldOf = (path: string) => path.split("/")[0];

// Titles of the parts of an object field or of collection items, from the schema.
export function partTitles(d: Draft, f: BriefField): Record<string, string> {
  const p = d.schema?.properties?.[f];
  const props = (p?.type === "array" ? p.items?.properties : p?.properties) ?? {};
  return Object.fromEntries(Object.entries(props).map(([k, v]: [string, any]) => [k, v?.title || k]));
}

// Chat → canvas: open the brief and point at the field.
export function focusField(field: BriefField) {
  draft.set((d) => ({ focus: { field, n: (d.focus?.n ?? 0) + 1 }, pane: "agent" }));
}

// Canvas → chat: open the room a value came from.
export function showRoom(id: string) {
  if (draft.get().rooms.some((r) => r.id === id)) void selectRoom(id).then(() => draft.set({ pane: "chat" }));
}

function fromSnapshot(o: ObjectView): Partial<Draft> {
  const stages = o.stages.length ? o.stages : DEFAULT_STAGES;
  const known = new Map(state.rooms.map((r) => [r.id, r]));
  return {
    id: o.id,
    name: o.title || "Новый агент",
    attrs: o.attrs,
    members: o.members,
    fields: o.fields,
    schema: (o as ObjectView & { schema?: Record<string, any> }).schema,
    brief: o.state,
    sources: o.sources,
    missing: o.missing,
    rev: o.rev,
    stages,
    stage: Math.max(0, stages.findIndex((s) => s.id === o.stage)),
    rooms: o.conversations.map((c) => known.get(c.id) ?? newRoom(c)),
  };
}

function newRoom(c: Conversation): Room {
  return { id: c.id, title: c.title, messages: [], loaded: false, running: false };
}

let unsubscribe: (() => void) | undefined;

// load reads the snapshot of an object and follows its events from there.
async function load(id: string) {
  unsubscribe?.();
  const o = await api<ObjectView>("GET", `/objects/${id}`);
  draft.set({ ...fromSnapshot(o), status: "ready", error: undefined });
  unsubscribe = subscribe(o.id, o.last_seq, {
    event: apply,
    reset: () => void load(o.id).catch(fail),
    status: (live) => draft.set({ live }),
  });
  const d = draft.get();
  if (!d.rooms.length) await openRoom();
  else await selectRoom(d.rooms.some((r) => r.id === d.room) ? d.room : d.rooms[0].id);
}

function fail(e: unknown) {
  if (e instanceof ApiError && e.status === 401) {
    draft.set({ status: "token", error: "Токен не подошел" });
    return;
  }
  draft.set({ status: "error", error: e instanceof Error ? e.message : String(e) });
}

// boot opens the newest draft of the person, or starts one.
export async function boot() {
  if (!token()) {
    draft.set({ status: "token" });
    return;
  }
  draft.set({ status: "loading", error: undefined });
  try {
    const me = await api<{ person: string }>("GET", "/me");
    draft.set({ me: me.person });
    const { objects } = await api<{ objects: ObjectSummary[] }>(
      "GET",
      `/agents/${BUILDER.agent}/objects?connection=${BUILDER.connection}&mine=1&limit=1`,
    );
    const id = objects[0]?.id ?? (await createObject()).id;
    await load(id);
  } catch (e) {
    fail(e);
  }
}

async function createObject(): Promise<ObjectView> {
  return api<ObjectView>("POST", `/agents/${BUILDER.agent}/objects`, { connection: BUILDER.connection, title: "Новый агент" });
}

// newAgent starts another draft.
export async function newAgent() {
  try {
    draft.set({ ...empty, me: state.me, pane: state.pane });
    await load((await createObject()).id);
  } catch (e) {
    fail(e);
  }
}

// A new room opens with what the brief still lacks: the core writes the first message.
export async function openRoom() {
  const { conversation, message } = await api<{ conversation: Conversation; message: Message }>(
    "POST",
    `/objects/${state.id}/conversations`,
    {},
  );
  upsertRoom(conversation, { loaded: true });
  addMessage(message);
  draft.set({ room: conversation.id });
}

export async function selectRoom(id: string) {
  draft.set({ room: id });
  const room = state.rooms.find((r) => r.id === id);
  if (!room || room.loaded) return;
  const { messages } = await api<{ messages: Message[] }>("GET", `/objects/${state.id}/conversations/${id}/messages?limit=200`);
  updateRoom(id, (r) => ({ loaded: true, messages: merge(messages, r.messages) }));
}

// say sends a message; it shows at once and is replaced by the core's copy from the stream.
export async function say(roomId: string, text: string) {
  const id = crypto.randomUUID();
  const mine: Message = { seq: 0, message_id: id, conversation: roomId, role: "user", author: state.me ?? "", text, fields: [], at: new Date().toISOString() };
  const room = state.rooms.find((r) => r.id === roomId);
  updateRoom(roomId, (r) => ({ messages: [...r.messages, mine], running: true }));
  try {
    await api("POST", `/objects/${state.id}/conversations/${roomId}/messages`, { message_id: id, text });
    if (room && !room.title) {
      const title = text.length > 38 ? text.slice(0, 38) + "…" : text;
      updateRoom(roomId, () => ({ title }));
      void api("PATCH", `/objects/${state.id}/conversations/${roomId}`, { title }).catch(() => undefined);
    }
  } catch (e) {
    updateRoom(roomId, (r) => ({ messages: r.messages.filter((m) => m.message_id !== id), running: false }));
    draft.set({ notice: e instanceof Error ? e.message : String(e) });
  }
}

// editField saves a value by hand. It conflicts only when the same field changed since this copy.
export async function editField(field: BriefField, value: unknown) {
  try {
    const o = await api<ObjectView>("PATCH", `/objects/${state.id}/state`, {
      fields: { [field]: { value, rev: state.sources[field]?.rev ?? 0 } },
    });
    draft.set({ ...fromSnapshot(o), lastFilled: field });
  } catch (e) {
    if (e instanceof ApiError && e.code === "field_conflict") {
      draft.set((d) => ({ notice: `«${label(d, field)}» уже изменили – показано новое значение`, brief: { ...d.brief, [field]: e.body.current } }));
      return;
    }
    draft.set({ notice: e instanceof Error ? e.message : String(e) });
  }
}

export async function rename(title: string) {
  try {
    const o = await api<ObjectView>("PATCH", `/objects/${state.id}`, { title });
    draft.set({ name: o.title });
  } catch (e) {
    draft.set({ notice: e instanceof Error ? e.message : String(e) });
  }
}

function upsertRoom(c: Conversation, patch: Partial<Room> = {}) {
  draft.set((d) => {
    const exists = d.rooms.some((r) => r.id === c.id);
    return {
      rooms: exists
        ? d.rooms.map((r) => (r.id === c.id ? { ...r, title: c.title || r.title, ...patch } : r))
        : [{ ...newRoom(c), ...patch }, ...d.rooms],
    };
  });
}

function updateRoom(id: string, patch: (r: Room) => Partial<Room>) {
  draft.set((d) => ({ rooms: d.rooms.map((r) => (r.id === id ? { ...r, ...patch(r) } : r)) }));
}

// merge keeps one copy of each message: the core's copy replaces the optimistic one by message_id.
function merge(core: Message[], local: Message[]): Message[] {
  const out = [...core];
  for (const m of local) {
    if (m.seq && out.some((x) => x.seq === m.seq)) continue;
    if (!m.seq && out.some((x) => x.message_id && x.message_id === m.message_id)) continue;
    out.push(m);
  }
  return out.sort((a, b) => (a.seq || Infinity) - (b.seq || Infinity));
}

function addMessage(m: Message) {
  updateRoom(m.conversation, (r) => ({ messages: merge([m], r.messages) }));
}

// apply folds an event of the object into the store.
function apply(e: ObjectEvent) {
  const d = e.data;
  switch (e.type) {
    case "object":
      draft.set((s) => ({ name: d.title ?? s.name, attrs: d.attrs ?? s.attrs }));
      break;
    case "members":
      draft.set({ members: d.members });
      break;
    case "conversation":
      if (d.archived_at) draft.set((s) => ({ rooms: s.rooms.filter((r) => r.id !== d.id) }));
      else upsertRoom(d);
      break;
    case "message":
      addMessage(d);
      break;
    case "run":
      if (d.conversation) updateRoom(d.conversation, () => ({ running: d.state !== "done" }));
      if (d.state === "done" && d.status && d.status !== "ok" && d.conversation) {
        addMessage({
          seq: 0,
          conversation: d.conversation,
          role: "agent",
          author: "",
          text: `Не получилось ответить (${d.status})${d.error ? `: ${d.error}` : ""}. Попробуй переформулировать.`,
          fields: [],
          at: new Date().toISOString(),
          message_id: `failed-${d.run_id}`,
        });
      }
      break;
    case "state": {
      const changed: string[] = d.changed ?? [];
      draft.set((s) => {
        const sources = { ...s.sources };
        for (const p of changed) {
          if (d.source === "person") for (const k of Object.keys(sources)) if (k.startsWith(fieldOf(p) + "/")) delete sources[k];
          sources[p] = { rev: d.rev, source: d.source, conversation_id: d.conversation, run_id: d.run_id, by: d.by, at: new Date().toISOString() };
        }
        const brief = { ...s.brief };
        for (const [k, v] of Object.entries(d.values ?? {})) {
          if (v === null || v === undefined) delete brief[k];
          else brief[k] = v;
        }
        return { brief, sources, missing: d.missing ?? s.missing, rev: d.rev, lastFilled: changed[0] ? fieldOf(changed[0]) : s.lastFilled };
      });
      break;
    }
  }
}

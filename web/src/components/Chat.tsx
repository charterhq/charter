import {
  AssistantRuntimeProvider,
  AuiIf,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  WebSpeechDictationAdapter,
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { ArrowRight, ArrowUp, Check, ChevronDown, Mic, Plus, Square } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Boat } from "./Mascot";
import { locale, useT, type Messages } from "@/i18n";
import type { Message } from "@/lib/api";
import { fieldOf, focusField, label, openRoom, say, selectRoom, useDraft, type Room } from "@/lib/store";
import { cn } from "@/lib/utils";

export function Chat() {
  const t = useT();
  const d = useDraft();
  const room = d.rooms.find((r) => r.id === d.room);

  return (
    <section className="flex min-h-0 flex-1 flex-col rounded-xl border bg-card shadow-panel">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <RoomSwitcher rooms={d.rooms} current={d.room} live={d.live} />
        <button
          onClick={() => void openRoom()}
          title={t.chat.newRoomHint}
          className="ml-auto flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground"
        >
          <Plus size={14} />
          {t.chat.newRoom}
        </button>
      </div>
      {room && <RoomThread key={room.id} room={room} />}
    </section>
  );
}

// Rooms keep the chat short; the brief carries what was decided, so switching loses nothing.
function RoomSwitcher({ rooms, current, live }: { rooms: Room[]; current: string; live: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", esc);
    };
  }, []);
  const room = rooms.find((r) => r.id === current);

  return (
    <div ref={ref} className="relative min-w-0">
      <button
        onClick={() => setOpen(!open)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex max-w-full min-w-0 items-center gap-2 rounded-md px-2 py-1 text-[13px] font-medium text-muted-foreground hover:bg-surface hover:text-foreground"
      >
        <span
          title={live ? t.chat.live : t.chat.offline}
          className={cn(
            "size-[7px] shrink-0 rounded-full",
            live ? "bg-success shadow-[0_0_0_3px_color-mix(in_oklab,var(--success)_20%,transparent)]" : "bg-border-strong",
          )}
        />
        <span className="min-w-0 truncate">{roomTitle(t, room, rooms)}</span>
        <ChevronDown size={13} className="shrink-0" />
      </button>
      {open && (
        <div className="absolute left-0 top-full z-20 mt-1 w-72 rounded-xl border bg-popover p-1.5 shadow-floating">
          <div className="px-2 pb-1.5 pt-1 text-[12px] text-faint">{t.chat.rooms}</div>
          {rooms.map((r) => (
            <button
              key={r.id}
              onClick={() => {
                void selectRoom(r.id);
                setOpen(false);
              }}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[14px] text-muted-foreground hover:bg-surface hover:text-foreground",
                r.id === current && "bg-surface text-foreground",
              )}
            >
              <span className="min-w-0 flex-1 truncate">{roomTitle(t, r, rooms)}</span>
              {r.loaded && <span className="text-[12px] text-faint">{r.messages.length}</span>}
              {r.id === current && <Check size={13} className="text-primary" />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function roomTitle(t: Messages, room: Room | undefined, rooms: Room[]) {
  if (!room) return t.chat.room;
  return room.title || t.chat.roomN(rooms.length - rooms.indexOf(room));
}

function convert(m: Message): ThreadMessageLike {
  return {
    role: m.role === "agent" ? "assistant" : "user",
    content: [{ type: "text", text: m.text }],
    id: m.message_id || `seq-${m.seq}`,
    createdAt: new Date(m.at),
    metadata: { custom: { fields: m.fields } },
  };
}

// The thread shows the room's messages from the core; a sent message goes to the core and its
// answer comes back through the object's event stream.
function RoomThread({ room }: { room: Room }) {
  // Dictation listens in the language of the interface.
  const lang = locale();
  const dictation = useMemo(
    () => (WebSpeechDictationAdapter.isSupported() ? new WebSpeechDictationAdapter({ language: lang, interimResults: true }) : undefined),
    [lang],
  );
  const runtime = useExternalStoreRuntime<Message>({
    messages: room.messages,
    isRunning: room.running,
    isLoading: !room.loaded,
    convertMessage: convert,
    onNew: async (msg) => {
      const text = msg.content.map((p) => (p.type === "text" ? p.text : "")).join(" ").trim();
      if (text) await say(room.id, text);
    },
    adapters: { dictation },
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
        <ThreadPrimitive.Viewport className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? (
                <MessagePrimitive.Root className="flex min-w-0 max-w-[85%] flex-col items-end gap-1.5 self-end">
                  <div className="max-w-full rounded-xl rounded-br-sm border bg-surface px-3 py-2 text-[15px] [overflow-wrap:anywhere] whitespace-pre-wrap">
                    <MessagePrimitive.Parts />
                  </div>
                </MessagePrimitive.Root>
              ) : (
                <MessagePrimitive.Root>
                  <Bot fields={(message.metadata.custom?.fields as string[] | undefined) ?? []}>
                    <MessagePrimitive.Parts />
                  </Bot>
                </MessagePrimitive.Root>
              )
            }
          </ThreadPrimitive.Messages>
          {room.running && <Typing />}
        </ThreadPrimitive.Viewport>
        <Composer />
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

function Typing() {
  const t = useT();
  return (
    <div className="flex items-center gap-2.5 text-[13px] text-faint" aria-live="polite">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border bg-surface">
        <Boat size={18} />
      </span>
      {t.chat.thinking}
    </div>
  );
}

const iconButton =
  "grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground hover:bg-card hover:text-foreground disabled:opacity-40 disabled:hover:bg-transparent";

// Attachments come back when the core keeps files of an object; dictation stays in the browser.
function Composer() {
  const t = useT();
  return (
    <ComposerPrimitive.Root className="m-3 rounded-xl border bg-surface focus-within:border-primary">
      <ComposerPrimitive.Input
        rows={1}
        aria-label={t.chat.input}
        placeholder={t.chat.inputPlaceholder}
        className="mx-1.5 mt-1.5 mb-1 max-h-48 min-h-9 w-[calc(100%-0.75rem)] resize-none bg-transparent px-1.5 pt-1 text-[15px] outline-none placeholder:text-faint"
      />
      <div className="flex items-center gap-0.5 p-1.5 pt-0">
        <AuiIf condition={(s) => s.composer.dictation == null}>
          <ComposerPrimitive.Dictate className={iconButton} aria-label={t.chat.dictate} title={t.chat.dictateHint}>
            <Mic size={15} />
          </ComposerPrimitive.Dictate>
        </AuiIf>
        <AuiIf condition={(s) => s.composer.dictation != null}>
          <ComposerPrimitive.StopDictation className={cn(iconButton, "text-destructive")} title={t.chat.stopDictation} aria-label={t.chat.stopDictation}>
            <Square size={13} className="animate-pulse fill-current" />
          </ComposerPrimitive.StopDictation>
        </AuiIf>
        <ComposerPrimitive.Send aria-label={t.chat.send} className="ml-auto grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground hover:bg-primary-hover disabled:opacity-40">
          <ArrowUp size={15} />
        </ComposerPrimitive.Send>
      </div>
    </ComposerPrimitive.Root>
  );
}

function Bot({ children, fields }: { children: React.ReactNode; fields: string[] }) {
  const t = useT();
  const d = useDraft();
  const cards = [...new Set(fields.map(fieldOf))];
  return (
    <div className="flex items-start gap-2.5">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border bg-surface">
        <Boat size={18} />
      </span>
      <div className="min-w-0 pt-0.5 text-[15px] [overflow-wrap:anywhere] whitespace-pre-wrap">
        <div className="mb-0.5 text-[12px] font-semibold text-foreground">{t.chat.builder}</div>
        {children}
        {cards.length > 0 && (
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {cards.map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => focusField(f)}
                className="flex items-center gap-1 rounded-md border bg-surface px-2 py-0.5 text-[12px] text-muted-foreground hover:border-border-strong hover:text-foreground"
              >
                <ArrowRight size={12} aria-hidden />
                {label(d, f)}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

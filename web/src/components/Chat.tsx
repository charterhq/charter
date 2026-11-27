import {
  AssistantRuntimeProvider,
  AttachmentPrimitive,
  AuiIf,
  ComposerPrimitive,
  CompositeAttachmentAdapter,
  MessagePrimitive,
  SimpleImageAttachmentAdapter,
  SimpleTextAttachmentAdapter,
  ThreadPrimitive,
  WebSpeechDictationAdapter,
  useLocalRuntime,
} from "@assistant-ui/react";
import { ArrowRight, ArrowUp, Check, ChevronDown, Mic, Paperclip, Plus, Square, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Boat } from "./Mascot";
import { builderFor } from "@/lib/builder-mock";
import { BRIEF_LABELS, draft, focusField, openRoom, useDraft, type BriefField, type Room } from "@/lib/store";
import { cn } from "@/lib/utils";

export function Chat() {
  const d = useDraft();
  useEffect(() => {
    if (!draft.get().rooms.length) openRoom();
  }, []);
  const room = d.rooms.find((r) => r.id === d.room);

  return (
    <section className="flex min-h-0 flex-1 flex-col rounded-xl border bg-card shadow-panel">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <RoomSwitcher rooms={d.rooms} current={d.room} />
        <button
          onClick={() => openRoom()}
          title="Новая беседа: бриф и все решения сохраняются"
          className="ml-auto flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground"
        >
          <Plus size={14} />
          Беседа
        </button>
      </div>
      {room && <RoomThread key={room.id} room={room} />}
    </section>
  );
}

// Rooms keep the chat short; the brief carries what was decided, so switching loses nothing.
function RoomSwitcher({ rooms, current }: { rooms: Room[]; current: string }) {
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
        <span className="size-[7px] shrink-0 rounded-full bg-success shadow-[0_0_0_3px_color-mix(in_oklab,var(--success)_20%,transparent)]" />
        <span className="min-w-0 truncate">{room?.title ?? "Беседа"}</span>
        <ChevronDown size={13} className="shrink-0" />
      </button>
      {open && (
        <div className="absolute left-0 top-full z-20 mt-1 w-72 rounded-xl border bg-card p-1.5 shadow-panel">
          <div className="px-2 pb-1.5 pt-1 text-[12px] text-faint">Беседы с билдером · память – в брифе</div>
          {rooms.map((r) => (
            <button
              key={r.id}
              onClick={() => {
                draft.set({ room: r.id });
                setOpen(false);
              }}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[14px] text-muted-foreground hover:bg-surface hover:text-foreground",
                r.id === current && "bg-surface text-foreground",
              )}
            >
              <span className="min-w-0 flex-1 truncate">{r.title}</span>
              <span className="text-[12px] text-faint">{r.messages.length}</span>
              {r.id === current && <Check size={13} className="text-primary" />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function RoomThread({ room }: { room: Room }) {
  const adapter = useMemo(() => builderFor(room.id), [room.id]);
  const runtime = useLocalRuntime(adapter, {
    initialMessages: room.messages.map((m) => ({ role: m.role, content: m.text, metadata: { custom: m.field ? { field: m.field } : {} } })),
    adapters: {
      attachments: new CompositeAttachmentAdapter([new SimpleImageAttachmentAdapter(), new SimpleTextAttachmentAdapter()]),
      dictation: WebSpeechDictationAdapter.isSupported()
        ? new WebSpeechDictationAdapter({ language: "ru-RU", interimResults: true })
        : undefined,
    },
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
        <ThreadPrimitive.Viewport className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? (
                <MessagePrimitive.Root className="flex min-w-0 max-w-[85%] flex-col items-end gap-1.5 self-end">
                  <MessagePrimitive.Attachments>
                    {() => (
                      <AttachmentPrimitive.Root className="rounded-md border bg-surface px-2 py-0.5 text-[12px] text-muted-foreground">
                        <AttachmentPrimitive.Name />
                      </AttachmentPrimitive.Root>
                    )}
                  </MessagePrimitive.Attachments>
                  <div className="max-w-full rounded-xl rounded-br-sm border bg-surface px-3 py-2 text-[15px] [overflow-wrap:anywhere]">
                    <MessagePrimitive.Parts />
                  </div>
                </MessagePrimitive.Root>
              ) : (
                <MessagePrimitive.Root>
                  <Bot field={message.metadata.custom?.field as BriefField | undefined}>
                    <MessagePrimitive.Parts />
                  </Bot>
                </MessagePrimitive.Root>
              )
            }
          </ThreadPrimitive.Messages>
        </ThreadPrimitive.Viewport>
        <Composer />
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  );
}

const iconButton =
  "grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground hover:bg-card hover:text-foreground disabled:opacity-40 disabled:hover:bg-transparent";

function Composer() {
  return (
    <ComposerPrimitive.Root className="m-3 rounded-xl border bg-surface focus-within:border-primary">
      <ComposerPrimitive.AttachmentDropzone className="flex flex-col">
        <div className="flex flex-wrap gap-1.5 px-2 pt-2 empty:hidden">
          <ComposerPrimitive.Attachments>
            {() => (
              <AttachmentPrimitive.Root className="flex items-center gap-1.5 rounded-md border bg-card py-0.5 pl-2 pr-1 text-[12px] text-muted-foreground">
                <AttachmentPrimitive.Name />
                <AttachmentPrimitive.Remove aria-label="Убрать вложение" className="rounded p-0.5 hover:bg-surface hover:text-foreground">
                  <X size={11} />
                </AttachmentPrimitive.Remove>
              </AttachmentPrimitive.Root>
            )}
          </ComposerPrimitive.Attachments>
        </div>
        <ComposerPrimitive.Input
          rows={1}
          aria-label="Сообщение билдеру"
          placeholder="Напиши или скажи, что хочешь…"
          className="mx-1.5 mt-1.5 mb-1 max-h-48 min-h-9 resize-none bg-transparent px-1.5 pt-1 text-[15px] outline-none placeholder:text-faint"
        />
        <div className="flex items-center gap-0.5 p-1.5 pt-0">
          <ComposerPrimitive.AddAttachment className={iconButton} title="Приложить файл или картинку" aria-label="Приложить файл или картинку">
            <Paperclip size={15} />
          </ComposerPrimitive.AddAttachment>
          <AuiIf condition={(s) => s.composer.dictation == null}>
            <ComposerPrimitive.Dictate
              className={iconButton}
              aria-label="Сказать голосом"
              title="Сказать голосом (в этом браузере может быть недоступно)"
            >
              <Mic size={15} />
            </ComposerPrimitive.Dictate>
          </AuiIf>
          <AuiIf condition={(s) => s.composer.dictation != null}>
            <ComposerPrimitive.StopDictation className={cn(iconButton, "text-destructive")} title="Остановить запись" aria-label="Остановить запись">
              <Square size={13} className="animate-pulse fill-current" />
            </ComposerPrimitive.StopDictation>
          </AuiIf>
          <ComposerPrimitive.Send aria-label="Отправить" className="ml-auto grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground hover:bg-primary-hover disabled:opacity-40">
            <ArrowUp size={15} />
          </ComposerPrimitive.Send>
        </div>
      </ComposerPrimitive.AttachmentDropzone>
    </ComposerPrimitive.Root>
  );
}

function Bot({ children, field }: { children: React.ReactNode; field?: BriefField }) {
  return (
    <div className="flex items-start gap-2.5">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border bg-surface">
        <Boat size={18} />
      </span>
      <div className="min-w-0 pt-0.5 text-[15px] [overflow-wrap:anywhere]">
        <div className="mb-0.5 text-[12px] font-semibold text-sky">Билдер</div>
        {children}
        {field && (
          <button
            type="button"
            onClick={() => focusField(field)}
            className="mt-1.5 flex items-center gap-1 rounded-md border bg-surface px-2 py-0.5 text-[12px] text-muted-foreground hover:border-border-strong hover:text-foreground"
          >
            <ArrowRight size={12} aria-hidden />
            {BRIEF_LABELS[field]}
          </button>
        )}
      </div>
    </div>
  );
}

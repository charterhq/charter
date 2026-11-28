import { ArrowUp, Plus, Square } from "lucide-react";
import { useLayoutEffect, useRef, type ReactNode } from "react";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// The prompt box of an agent chat: the text grows with what is typed, tools sit in the toolbar,
// and while a reply is under way the send button turns into a stop ringed by its progress.
// Data and handlers come through props, so any runtime can drive it.

export type Suggestion = { id: string; label: string; icon?: ReactNode };

export type AiComposerProps = {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: string) => void;
  label: string;
  placeholder?: string;
  running?: boolean;
  // 0..1 when known; without it the ring turns.
  progress?: number;
  onStop?: () => void;
  onAttach?: () => void;
  // Toggles and menus between attach and send: "Web search", a model picker, dictation.
  tools?: ReactNode;
  trailing?: ReactNode;
  // Starting points under the box; a pick fills the box.
  suggestions?: Suggestion[];
  disabled?: boolean;
};

export function AiComposer(p: AiComposerProps) {
  const t = useT();
  const area = useRef<HTMLTextAreaElement>(null);
  // The box grows with its text up to a limit, then scrolls.
  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 192)}px`;
  }, [p.value]);
  const canSend = !p.disabled && !p.running && p.value.trim() !== "";
  const send = () => canSend && p.onSubmit(p.value.trim());

  return (
    <div className="min-w-0">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
        className="rounded-2xl border bg-card shadow-[var(--elevation-raised)] transition-[border-color] focus-within:border-border-strong"
      >
        <textarea
          ref={area}
          rows={1}
          value={p.value}
          disabled={p.disabled}
          aria-label={p.label}
          placeholder={p.placeholder}
          onChange={(e) => p.onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              send();
            }
          }}
          className="block max-h-48 min-h-11 w-full resize-none bg-transparent px-4 pt-3 pb-1 text-[15px] outline-none placeholder:text-faint"
        />
        <div className="flex items-center gap-1 p-2 pt-1">
          {p.onAttach && (
            <button type="button" onClick={p.onAttach} aria-label={t.blocks.attach} title={t.blocks.attach} className={tool}>
              <Plus size={16} aria-hidden />
            </button>
          )}
          <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">{p.tools}</div>
          {p.trailing}
          {p.running && p.onStop ? (
            <button type="button" onClick={p.onStop} aria-label={t.blocks.stop} title={t.blocks.stop} className="relative grid size-8 shrink-0 place-items-center rounded-full bg-surface text-foreground">
              <StopRing progress={p.progress} />
              <Square size={10} className="fill-current" aria-hidden />
            </button>
          ) : (
            <button
              type="submit"
              disabled={!canSend}
              aria-label={t.blocks.send}
              title={t.blocks.send}
              className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-primary-foreground hover:bg-primary-hover disabled:bg-surface disabled:text-faint"
            >
              <ArrowUp size={16} aria-hidden />
            </button>
          )}
        </div>
      </form>
      {p.suggestions && p.suggestions.length > 0 && (
        <ul className="mt-3 flex flex-wrap justify-center gap-2">
          {p.suggestions.map((s) => (
            <li key={s.id}>
              <button
                type="button"
                onClick={() => {
                  p.onChange(s.label);
                  area.current?.focus();
                }}
                className="flex items-center gap-2 rounded-full border bg-card px-3 py-1.5 text-[13px] text-muted-foreground hover:border-border-strong hover:text-foreground"
              >
                {s.icon && <span aria-hidden className="text-faint">{s.icon}</span>}
                {s.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

const tool = "grid size-8 shrink-0 place-items-center rounded-full text-muted-foreground hover:bg-surface hover:text-foreground";

// ToolToggle is a pill in the toolbar that is on or off: "Web search", "Extended thinking".
export function ToolToggle({ on, onToggle, icon, children }: { on: boolean; onToggle: (on: boolean) => void; icon?: ReactNode; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={() => onToggle(!on)}
      className={cn(
        "flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-[13px] text-muted-foreground hover:text-foreground",
        on && "border-border-strong bg-surface text-foreground",
      )}
    >
      {icon && <span aria-hidden>{icon}</span>}
      {children}
    </button>
  );
}

// The ring around the stop button: a known share of the reply, or a turning arc.
function StopRing({ progress }: { progress?: number }) {
  const r = 15;
  const c = 2 * Math.PI * r;
  const known = progress !== undefined;
  return (
    <svg viewBox="0 0 32 32" aria-hidden className={cn("absolute inset-0 -rotate-90", !known && "animate-spin [animation-duration:1.2s]")}>
      <circle cx="16" cy="16" r={r} fill="none" stroke="var(--border-strong)" strokeWidth="1.5" />
      <circle
        cx="16"
        cy="16"
        r={r}
        fill="none"
        stroke="var(--foreground)"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeDasharray={c}
        strokeDashoffset={known ? c * (1 - Math.max(0, Math.min(1, progress))) : c * 0.75}
        style={known ? { transition: "stroke-dashoffset .3s" } : undefined}
      />
    </svg>
  );
}

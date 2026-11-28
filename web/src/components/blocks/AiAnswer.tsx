import { Check, ChevronDown, Copy, RotateCcw } from "lucide-react";
import { useEffect, useId, useState, type ReactNode } from "react";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// An answer set like a short article: how long it thought, its sources, the text with citations,
// and actions under it. Data comes through props; <Claim> and <Cite> mark up the text.

export type AnswerSource = { id: string; title: string; site: string; mark?: ReactNode; href?: string };

export type AiAnswerProps = {
  question?: ReactNode;
  // "9s", already formatted; with reasoning the line opens to show it.
  thought?: string;
  reasoning?: ReactNode;
  sources?: AnswerSource[];
  children: ReactNode;
  // Plain text for the copy button.
  copyText?: string;
  onRegenerate?: () => void;
};

export function AiAnswer({ question, thought, reasoning, sources = [], children, copyText, onRegenerate }: AiAnswerProps) {
  const t = useT();
  const [why, setWhy] = useState(false);
  const [list, setList] = useState(false);
  const whyId = useId();
  const listId = useId();
  return (
    <article className="min-w-0">
      {question && <h2 className="mb-3 text-[20px] font-semibold tracking-tight">{question}</h2>}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[13px] text-muted-foreground">
        {thought &&
          (reasoning ? (
            <button type="button" aria-expanded={why} aria-controls={whyId} onClick={() => setWhy(!why)} className="flex items-center gap-1 hover:text-foreground">
              {t.blocks.thoughtFor(thought)}
              <ChevronDown size={13} aria-hidden className={cn("transition-transform duration-200", why && "rotate-180")} />
            </button>
          ) : (
            <span>{t.blocks.thoughtFor(thought)}</span>
          ))}
        {sources.length > 0 && (
          <button type="button" aria-expanded={list} aria-controls={listId} onClick={() => setList(!list)} className="flex items-center gap-2 hover:text-foreground">
            <span aria-hidden className="flex -space-x-1">
              {sources.slice(0, 4).map((s) => (
                <span key={s.id} className="grid size-4 place-items-center rounded-full bg-card text-[10px] font-semibold">
                  {s.mark ?? s.site[0]?.toUpperCase()}
                </span>
              ))}
            </span>
            {t.blocks.sources(sources.length)}
            <ChevronDown size={13} aria-hidden className={cn("transition-transform duration-200", list && "rotate-180")} />
          </button>
        )}
      </div>
      {why && reasoning && (
        <div id={whyId} className="mt-2 animate-[fade-in_.16s_ease-out] border-l pl-3 text-[13px] text-muted-foreground">
          {reasoning}
        </div>
      )}
      {list && (
        <ol id={listId} className="mt-2 grid animate-[fade-in_.16s_ease-out] gap-1.5 sm:grid-cols-2">
          {sources.map((s, i) => (
            <li key={s.id} className="flex min-w-0 items-center gap-2 rounded-lg border bg-card px-2.5 py-1.5 text-[12px]">
              <span className="w-3 shrink-0 tabular-nums text-faint">{i + 1}</span>
              {s.href ? (
                <a href={s.href} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate text-foreground hover:underline">
                  {s.title}
                </a>
              ) : (
                <span className="min-w-0 flex-1 truncate text-foreground">{s.title}</span>
              )}
              <span className="shrink-0 text-faint">{s.site}</span>
            </li>
          ))}
        </ol>
      )}

      <div className="mt-4 space-y-3 text-[15px] leading-relaxed [overflow-wrap:anywhere]">{children}</div>

      {(copyText !== undefined || onRegenerate) && (
        <div className="mt-3 flex gap-0.5">
          {copyText !== undefined && <CopyButton text={copyText} />}
          {onRegenerate && (
            <button type="button" onClick={onRegenerate} aria-label={t.blocks.regenerate} title={t.blocks.regenerate} className={action}>
              <RotateCcw size={14} aria-hidden />
            </button>
          )}
        </div>
      )}
    </article>
  );
}

const action = "grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-surface hover:text-foreground";

function CopyButton({ text }: { text: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const id = window.setTimeout(() => setDone(false), 1500);
    return () => window.clearTimeout(id);
  }, [done]);
  const label = done ? t.blocks.copied : t.blocks.copy;
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={() => void navigator.clipboard?.writeText(text).then(() => setDone(true), () => undefined)}
      className={action}
    >
      {done ? <Check size={14} className="text-success" aria-hidden /> : <Copy size={14} aria-hidden />}
    </button>
  );
}

// Claim is a sentence that a citation supports: it lights up while the citation is pointed at.
export function Claim({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-sm transition-colors duration-150 has-[[data-cite]:hover]:bg-[color-mix(in_oklab,var(--gradient-from)_55%,transparent)] has-[[data-cite]:focus-visible]:bg-[color-mix(in_oklab,var(--gradient-from)_55%,transparent)]">
      {children}
    </span>
  );
}

// Cite is a numbered chip after a claim that names its source.
export function Cite({ n, source }: { n: number; source: AnswerSource }) {
  const t = useT();
  const label = t.blocks.source(n, source.title);
  const chip =
    "mx-0.5 inline-grid min-w-4 place-items-center rounded bg-surface px-1 align-[1px] text-[11px] leading-4 tabular-nums text-muted-foreground hover:bg-border hover:text-foreground";
  return source.href ? (
    <a data-cite href={source.href} target="_blank" rel="noreferrer" aria-label={label} title={label} className={chip}>
      {n}
    </a>
  ) : (
    <span data-cite tabIndex={0} role="note" aria-label={label} title={label} className={chip}>
      {n}
    </span>
  );
}

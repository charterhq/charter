import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { ArrowUpRight, ChevronDown, Link2, RotateCcw, ThumbsDown, ThumbsUp } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { Collapse } from "./Collapse";
import { CopyButton } from "./CopyButton";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// An answer set like a short article: how long it thought, its sources, the text with citations,
// actions under it and questions to ask next. Data comes through props; <Claim> and <Cite> mark up
// the text, <AnswerCode> shows a code block. Headings, lists and tables inside get the article style.

export type AnswerSource = {
  id: string;
  title: string;
  site: string;
  mark?: ReactNode;
  href?: string;
  // For the card a citation shows: who published it, a line from it, when.
  publisher?: string;
  snippet?: ReactNode;
  date?: string;
};

export type Feedback = "good" | "bad";

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
  feedback?: Feedback;
  onFeedback?: (f: Feedback | undefined) => void;
  shareLink?: string;
  related?: string[];
  onAsk?: (question: string) => void;
};

export function AiAnswer(p: AiAnswerProps) {
  const { question, thought, reasoning, sources = [], children, copyText, onRegenerate, feedback, onFeedback, shareLink, related = [], onAsk } = p;
  const t = useT();
  const [why, setWhy] = useState(false);
  const [list, setList] = useState(false);
  const whyId = useId();
  const listId = useId();
  const chevron = (open: boolean) => <ChevronDown size={14} aria-hidden className={cn("transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", open && "rotate-180")} />;
  return (
    <article className="min-w-0">
      {question && <h3 className="mb-3 text-[20px] font-semibold tracking-tight">{question}</h3>}
      {thought &&
        (reasoning ? (
          <button type="button" aria-expanded={why} aria-controls={whyId} onClick={() => setWhy(!why)} className="flex items-center gap-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground">
            {t.blocks.thoughtFor(thought)}
            {chevron(why)}
          </button>
        ) : (
          <span className="text-[13px] text-muted-foreground">{t.blocks.thoughtFor(thought)}</span>
        ))}
      {reasoning && (
        <Collapse id={whyId} open={why}>
          <div className="mt-2 space-y-2 border-l pl-3.5 text-[13px] leading-relaxed text-muted-foreground">{reasoning}</div>
        </Collapse>
      )}
      {sources.length > 0 && (
        <>
          <button
            type="button"
            aria-expanded={list}
            aria-controls={listId}
            onClick={() => setList(!list)}
            className="mt-3 flex items-center gap-2 rounded-full bg-surface px-2.5 py-1.5 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
          >
            <span aria-hidden className="flex -space-x-1">
              {sources.slice(0, 4).map((s) => (
                <span key={s.id} className="grid size-[18px] place-items-center rounded-full border border-surface bg-card text-[10px] font-semibold">
                  {s.mark ?? s.site[0]?.toUpperCase()}
                </span>
              ))}
            </span>
            {t.blocks.sources(sources.length)}
            {chevron(list)}
          </button>
          <Collapse id={listId} open={list}>
            <ol className="grid gap-2 pt-3 sm:grid-cols-2">
              {sources.map((s, i) => (
                <li key={s.id} className="min-w-0">
                  <SourceCard source={s} n={i + 1} />
                </li>
              ))}
            </ol>
          </Collapse>
        </>
      )}

      <div
        className={cn(
          "mt-5 space-y-3 text-[15px] leading-relaxed [overflow-wrap:anywhere]",
          "[&_h4]:mt-6 [&_h4]:text-[16px] [&_h4]:font-semibold [&_h4]:tracking-tight",
          "[&_ol]:list-decimal [&_ol]:space-y-2 [&_ol]:pl-6 [&_ul]:list-disc [&_ul]:space-y-2 [&_ul]:pl-6 [&_li]:pl-1 [&_li::marker]:text-muted-foreground",
          "[&_strong]:font-semibold [&_:not(pre)>code]:rounded [&_:not(pre)>code]:bg-surface [&_:not(pre)>code]:px-1.5 [&_:not(pre)>code]:py-0.5 [&_:not(pre)>code]:font-mono [&_:not(pre)>code]:text-[13px]",
          "[&_table]:w-full [&_table]:text-[13px] [&_table]:[overflow-wrap:normal] [&_td:first-child]:whitespace-nowrap [&_td]:border-t [&_td]:px-3.5 [&_td]:py-2.5 [&_th]:px-3.5 [&_th]:py-2.5 [&_th]:text-left [&_th]:font-normal [&_th]:text-muted-foreground [&_td:first-child]:text-muted-foreground",
        )}
      >
        {children}
      </div>

      {(copyText !== undefined || onRegenerate || onFeedback || shareLink) && (
        <div className="mt-4 flex gap-0.5">
          {copyText !== undefined && <CopyButton text={copyText} />}
          {onRegenerate && (
            <Action label={t.blocks.regenerate} onClick={onRegenerate}>
              <RotateCcw size={14} aria-hidden />
            </Action>
          )}
          {onFeedback && (
            <>
              <Action label={t.blocks.goodResponse} pressed={feedback === "good"} onClick={() => onFeedback(feedback === "good" ? undefined : "good")}>
                <ThumbsUp size={14} aria-hidden className={cn(feedback === "good" && "fill-current")} />
              </Action>
              <Action label={t.blocks.badResponse} pressed={feedback === "bad"} onClick={() => onFeedback(feedback === "bad" ? undefined : "bad")}>
                <ThumbsDown size={14} aria-hidden className={cn(feedback === "bad" && "fill-current")} />
              </Action>
            </>
          )}
          {shareLink && <CopyButton text={shareLink} label={t.blocks.copyLink} icon={<Link2 size={14} aria-hidden />} />}
        </div>
      )}

      {related.length > 0 && (
        <section className="mt-6">
          <h4 className="mb-1 text-[13px] font-semibold">{t.blocks.related}</h4>
          <ul>
            {related.map((q) => (
              <li key={q} className="border-t first:border-t-0">
                <button
                  type="button"
                  onClick={() => onAsk?.(q)}
                  className="group flex w-full items-center gap-3 py-2.5 text-left text-[14px] text-muted-foreground transition-colors hover:text-foreground"
                >
                  <span className="min-w-0 flex-1">{q}</span>
                  <ArrowUpRight size={15} aria-hidden className="shrink-0 text-faint transition-transform duration-200 group-hover:-translate-y-0.5 group-hover:translate-x-0.5" />
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </article>
  );
}

function Action({ label, pressed, onClick, children }: { label: string; pressed?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={pressed}
      title={label}
      className={cn("grid size-8 place-items-center rounded-md transition-colors hover:bg-surface hover:text-foreground", pressed ? "text-foreground" : "text-muted-foreground")}
    >
      {children}
    </button>
  );
}

const badge = "grid size-[18px] shrink-0 place-items-center rounded-full bg-surface text-[10px] tabular-nums text-muted-foreground";

function SourceCard({ source: s, n }: { source: AnswerSource; n: number }) {
  const t = useT();
  const inner = (
    <>
      <span className="flex items-center gap-2 text-[12px] text-muted-foreground">
        <span aria-hidden className="grid size-4 shrink-0 place-items-center">
          {s.mark ?? s.site[0]?.toUpperCase()}
        </span>
        <span className="min-w-0 flex-1 truncate">{s.site}</span>
        <span aria-hidden className={badge}>
          {n}
        </span>
      </span>
      <span className="mt-1 block truncate text-[13px] text-foreground">{s.title}</span>
    </>
  );
  const box = "block rounded-lg border bg-card px-3 py-2.5 transition-colors";
  return s.href ? (
    <a href={s.href} target="_blank" rel="noreferrer" aria-label={`${s.site} ${s.title} ${t.blocks.sourceN(n)}`} className={cn(box, "hover:border-border-strong hover:bg-surface/50")}>
      {inner}
    </a>
  ) : (
    <div className={box}>{inner}</div>
  );
}

// Claim is a sentence that a citation supports: it lights up while the citation is pointed at.
export function Claim({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-sm transition-colors duration-150 has-[[data-cite]:hover]:bg-surface has-[[data-cite]:focus-visible]:bg-surface">
      {children}
    </span>
  );
}

// Cite is a numbered chip after a claim that names its source; pointing at it shows the source card.
export function Cite({ n, source }: { n: number; source: AnswerSource }) {
  const t = useT();
  const label = t.blocks.source(n, source.title);
  const chip = cn(badge, "mx-0.5 inline-grid align-[1px] transition-colors hover:bg-border hover:text-foreground");
  const trigger = source.href ? (
    <a data-cite href={source.href} target="_blank" rel="noreferrer" aria-label={label} className={chip}>
      {n}
    </a>
  ) : (
    <span data-cite tabIndex={0} role="note" aria-label={label} className={chip}>
      {n}
    </span>
  );
  return (
    <TooltipPrimitive.Provider delayDuration={200} skipDelayDuration={300}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{trigger}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            side="bottom"
            sideOffset={8}
            collisionPadding={12}
            className="z-[90] w-72 rounded-xl border bg-card p-3.5 text-[13px] shadow-[var(--elevation-raised)] data-[state=closed]:animate-[fade-out_.1s_ease-in_forwards] data-[state=delayed-open]:animate-[pop-in_.15s_cubic-bezier(.16,1,.3,1)] data-[state=instant-open]:animate-[fade-in_.1s_ease-out]"
          >
            <span className="flex items-center gap-2 text-[12px] text-muted-foreground">
              <span aria-hidden className="grid size-4 shrink-0 place-items-center">
                {source.mark ?? source.site[0]?.toUpperCase()}
              </span>
              <span className="min-w-0 flex-1 truncate">{source.publisher ?? source.site}</span>
              <span className="tabular-nums text-faint">{n}</span>
            </span>
            <span className="mt-1.5 block font-semibold text-foreground">{source.title}</span>
            {source.snippet != null && <span className="mt-1 block text-muted-foreground">{source.snippet}</span>}
            <span className="mt-2 flex items-center gap-1.5 text-[12px] text-faint">
              {source.site}
              {source.date && (
                <>
                  <span aria-hidden>•</span>
                  {source.date}
                </>
              )}
            </span>
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
}

// AnswerTable frames a table in an answer; a wide one scrolls inside and never widens the page.
export function AnswerTable({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border [contain:inline-size]">
      <table>{children}</table>
    </div>
  );
}

// AnswerCode is a code block in an answer: the file name and language on top, copy on the right.
export function AnswerCode({ file, lang, code }: { file?: string; lang?: string; code: string }) {
  const t = useT();
  return (
    <div className="overflow-hidden rounded-xl border bg-card [contain:inline-size]">
      <div className="flex min-h-10 items-center gap-2 border-b pl-3.5 pr-1.5 text-[12px]">
        {file && <span className="font-mono text-foreground">{file}</span>}
        {lang && <span className="text-faint">{lang}</span>}
        <CopyButton text={code} label={t.blocks.codeCopy} small className="ml-auto" />
      </div>
      <pre className="overflow-x-auto px-3.5 py-3 font-mono text-[12.5px] leading-6 text-foreground">
        <code>{code}</code>
      </pre>
    </div>
  );
}

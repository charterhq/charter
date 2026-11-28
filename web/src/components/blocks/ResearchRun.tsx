import { ChevronDown, Square } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { Collapse } from "./Collapse";
import { StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// A search that plans queries, reads sources and then answers. The queries sit on a rail with the
// marks of the sites they hit; the sources fold into a pill and open into a list where each source
// shows how far it is read and opens to its snippet. Data comes through props.

export type ResearchQuery = { id: string; text: string; status: StepStatus; results?: number; marks?: ReactNode[] };
export type ResearchSource = {
  id: string;
  title: string;
  site: string;
  mark?: ReactNode;
  year?: string;
  snippet?: ReactNode;
  href?: string;
  // How much of it is read, 0 to 1; read sources are 1.
  progress: number;
};

export type ResearchRunProps = {
  question: ReactNode;
  running: boolean;
  elapsed?: string;
  queries: ResearchQuery[];
  sources: ResearchSource[];
  // The answer, once it streams in.
  answer?: ReactNode;
  // How many words of the answer are in, shown by the stop button.
  words?: number;
  onStop?: () => void;
};

export function ResearchRun({ question, running, elapsed, queries, sources, answer, words, onStop }: ResearchRunProps) {
  const t = useT();
  const [steps, setSteps] = useState(true);
  const [list, setList] = useState(false);
  const stepsId = useId();
  const listId = useId();
  const read = sources.filter((s) => s.progress >= 1).length;
  const dot = <span aria-hidden className="text-faint">•</span>;
  return (
    <article aria-busy={running || undefined} className="min-w-0">
      <h3 className="text-[20px] font-semibold tracking-tight">{question}</h3>
      <button
        type="button"
        aria-expanded={steps}
        aria-controls={stepsId}
        onClick={() => setSteps(!steps)}
        className="mt-2 flex items-center gap-2 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
      >
        {running ? (
          <>
            <span className="text-foreground">{t.blocks.researching}</span>
            {elapsed && (
              <>
                {dot}
                <span className="tabular-nums">{elapsed}</span>
              </>
            )}
            {sources.length > 0 && (
              <>
                {dot}
                <span>{t.blocks.read(read, sources.length)}</span>
              </>
            )}
          </>
        ) : (
          <>
            <span>{elapsed ? t.blocks.researchedFor(elapsed) : t.blocks.researched}</span>
            {sources.length > 0 && (
              <>
                {dot}
                <span>{t.blocks.sources(sources.length)}</span>
              </>
            )}
          </>
        )}
        <ChevronDown size={14} aria-hidden className={cn("transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", steps && "rotate-180")} />
      </button>

      <Collapse id={stepsId} open={steps}>
        <ul className="ml-1.5 mt-2 space-y-0.5 border-l pl-3.5">
          {queries.map((q) => (
            <li key={q.id} className="flex min-h-7 animate-[fade-in_.15s_cubic-bezier(.16,1,.3,1)] items-center gap-2.5 text-[13px]">
              <StatusIcon status={q.status} size={13} />
              <span className={cn("min-w-0 flex-1 truncate transition-colors duration-200", q.status === "running" ? "text-foreground" : "text-muted-foreground")}>{q.text}</span>
              {q.marks && q.marks.length > 0 && (
                <span aria-hidden className="hidden shrink-0 items-center gap-1 text-muted-foreground sm:flex">
                  {q.marks.slice(0, 4).map((mk, i) => (
                    <span key={i} className="grid size-3.5 animate-[fade-in_.15s_ease-out] place-items-center">
                      {mk}
                    </span>
                  ))}
                </span>
              )}
              {q.results !== undefined && <span className="shrink-0 text-[12px] tabular-nums text-faint">{t.blocks.results(q.results)}</span>}
            </li>
          ))}
        </ul>
      </Collapse>

      {sources.length > 0 && (
        <div className="mt-2.5">
          <button
            type="button"
            aria-expanded={list}
            aria-controls={listId}
            onClick={() => setList(!list)}
            className="flex items-center gap-2.5 rounded-full border px-3 py-1.5 text-[12px] text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground"
          >
            <span aria-hidden className="flex items-center gap-1">
              {sources.slice(0, 5).map((s) => (
                <span key={s.id} className="grid size-3.5 animate-[fade-in_.15s_ease-out] place-items-center">
                  {s.mark ?? s.site[0]?.toUpperCase()}
                </span>
              ))}
            </span>
            {running ? t.blocks.sourcesRead(read, sources.length) : t.blocks.sources(sources.length)}
            <ChevronDown size={13} aria-hidden className={cn("transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", list && "rotate-180")} />
          </button>
          <Collapse id={listId} open={list}>
            <ol className="mt-2 space-y-0.5">
              {sources.map((s, i) => (
                <Source key={s.id} source={s} n={i + 1} />
              ))}
            </ol>
          </Collapse>
        </div>
      )}

      {answer && <div className="mt-5 animate-[fade-in_.3s_ease-out] text-[15px] leading-relaxed">{answer}</div>}

      {running && (onStop || words !== undefined) && (
        <div className="mt-4 flex items-center gap-3 text-[13px] text-muted-foreground">
          {onStop && (
            <button type="button" onClick={onStop} className="-ml-1.5 flex items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors hover:bg-surface hover:text-foreground">
              <Square size={9} className="fill-current" aria-hidden />
              {t.blocks.stop}
            </button>
          )}
          {words !== undefined && <span className="tabular-nums text-faint">{t.blocks.words(words)}</span>}
        </div>
      )}
    </article>
  );
}

function Source({ source: s, n }: { source: ResearchSource; n: number }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const id = useId();
  const reading = s.progress < 1;
  const body = s.snippet != null || !!s.href;
  const label = `${t.blocks.sourceN(n)}${reading ? `, ${t.blocks.readingSource(Math.round(s.progress * 100))}` : ""}`;
  const head = (
    <>
      <span aria-hidden className="mt-0.5 grid size-4 shrink-0 place-items-center text-muted-foreground">
        {s.mark ?? s.site[0]?.toUpperCase()}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-foreground">{s.title}</span>
        <span className="flex items-center gap-1.5 text-[12px] text-faint">
          {s.site}
          {s.year && (
            <>
              <span aria-hidden>•</span>
              {s.year}
            </>
          )}
        </span>
      </span>
      <span aria-label={label} className="relative mt-0.5 grid size-5 shrink-0 place-items-center text-[10px] tabular-nums text-muted-foreground">
        <svg viewBox="0 0 20 20" aria-hidden className="absolute inset-0 -rotate-90">
          <circle cx="10" cy="10" r="8.75" fill="none" strokeWidth="1.25" className="stroke-border" />
          <circle
            cx="10"
            cy="10"
            r="8.75"
            fill="none"
            strokeWidth="1.25"
            pathLength={1}
            strokeDasharray={1}
            className="stroke-muted-foreground transition-[stroke-dashoffset] duration-500 ease-[cubic-bezier(.22,1,.36,1)]"
            style={{ strokeDashoffset: 1 - Math.min(1, Math.max(0, s.progress)) }}
          />
        </svg>
        <span aria-hidden>{n}</span>
      </span>
    </>
  );
  const row = "flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left";
  return (
    <li className="animate-[fade-in_.15s_cubic-bezier(.16,1,.3,1)]">
      {body ? (
        <button type="button" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)} className={cn(row, "transition-colors hover:bg-surface/70")}>
          {head}
        </button>
      ) : (
        <div className={row}>{head}</div>
      )}
      {body && (
        <Collapse id={id} open={open}>
          <div className="space-y-1.5 pb-2 pl-9 pr-2.5 text-[13px]">
            {s.snippet != null && <p className="text-muted-foreground">{s.snippet}</p>}
            {s.href && (
              <a href={s.href} target="_blank" rel="noreferrer" className="block truncate text-[12px] text-faint underline underline-offset-2 hover:text-foreground">
                {s.href.replace(/^https?:\/\//, "")}
              </a>
            )}
          </div>
        </Collapse>
      )}
    </li>
  );
}


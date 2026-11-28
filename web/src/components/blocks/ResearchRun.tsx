import { ChevronDown, Square } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { Spinner, StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// A search that plans queries, reads sources and then answers. Data comes through props.

export type ResearchQuery = { id: string; text: string; status: StepStatus; results?: number; mark?: ReactNode };
export type ResearchSource = { id: string; title: string; site: string; mark?: ReactNode; read: boolean };

export type ResearchRunProps = {
  question: ReactNode;
  running: boolean;
  elapsed?: string;
  queries: ResearchQuery[];
  sources: ResearchSource[];
  // The answer, once it streams in.
  answer?: ReactNode;
  onStop?: () => void;
};

export function ResearchRun({ question, running, elapsed, queries, sources, answer, onStop }: ResearchRunProps) {
  const t = useT();
  const [steps, setSteps] = useState(true);
  const [list, setList] = useState(false);
  const stepsId = useId();
  const listId = useId();
  const read = sources.filter((s) => s.read).length;
  return (
    <article aria-busy={running || undefined} className="min-w-0">
      <h2 className="text-[20px] font-semibold tracking-tight">{question}</h2>
      <button
        type="button"
        aria-expanded={steps}
        aria-controls={stepsId}
        onClick={() => setSteps(!steps)}
        className="mt-2 flex items-center gap-1.5 text-[13px] text-muted-foreground hover:text-foreground"
      >
        {running && <Spinner size={12} />}
        <span className={cn(running && "text-foreground")}>{running ? t.blocks.researching : t.blocks.researched}</span>
        {elapsed && <span className="tabular-nums text-faint">· {elapsed}</span>}
        {sources.length > 0 && <span className="text-faint">· {t.blocks.read(read, sources.length)}</span>}
        <ChevronDown size={13} aria-hidden className={cn("transition-transform duration-200", steps && "rotate-180")} />
      </button>

      {steps && (
        <div id={stepsId} className="mt-2 border-l pl-3">
          <ul className="space-y-0.5">
            {queries.map((q) => (
              <li key={q.id} className="flex min-h-7 animate-[fade-in_.2s_ease-out] items-center gap-2.5 text-[13px]">
                <StatusIcon status={q.status} size={13} />
                <span className={cn("min-w-0 flex-1 truncate", q.status === "running" ? "text-foreground" : "text-muted-foreground")}>{q.text}</span>
                {q.results !== undefined && (
                  <span className="flex shrink-0 items-center gap-1.5 text-[12px] tabular-nums text-faint">
                    {q.mark}
                    {t.blocks.results(q.results)}
                  </span>
                )}
              </li>
            ))}
          </ul>
          {sources.length > 0 && (
            <div className="mt-2">
              <button
                type="button"
                aria-expanded={list}
                aria-controls={listId}
                onClick={() => setList(!list)}
                className="flex items-center gap-2 rounded-full border px-2.5 py-1 text-[12px] text-muted-foreground hover:border-border-strong hover:text-foreground"
              >
                <span aria-hidden className="flex -space-x-1">
                  {sources.slice(0, 4).map((s) => (
                    <span key={s.id} className="grid size-4 place-items-center rounded-full bg-card">
                      {s.mark ?? s.site[0]?.toUpperCase()}
                    </span>
                  ))}
                </span>
                {t.blocks.read(read, sources.length)}
                <ChevronDown size={12} aria-hidden className={cn("transition-transform duration-200", list && "rotate-180")} />
              </button>
              {list && (
                <ul id={listId} className="mt-2 grid gap-1.5 sm:grid-cols-2">
                  {sources.map((s) => (
                    <li key={s.id} className={cn("flex min-w-0 items-center gap-2 rounded-lg border bg-card px-2.5 py-1.5 text-[12px]", !s.read && "border-dashed")}>
                      <span aria-hidden className="grid size-4 shrink-0 place-items-center">
                        {s.mark ?? s.site[0]?.toUpperCase()}
                      </span>
                      <span className="min-w-0 flex-1 truncate text-foreground">{s.title}</span>
                      <span className="shrink-0 text-faint">{s.site}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}

      {answer && <div className="mt-4 animate-[fade-in_.3s_ease-out] text-[15px] leading-relaxed">{answer}</div>}

      {running && onStop && (
        <button type="button" onClick={onStop} className="mt-4 flex items-center gap-1.5 rounded-md px-1.5 py-1 text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground">
          <Square size={10} className="fill-current" aria-hidden />
          {t.blocks.stop}
        </button>
      )}
    </article>
  );
}

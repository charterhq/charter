import { Check, Lock } from "lucide-react";
import { useState } from "react";
import { Boat } from "./Mascot";
import {
  BRIEF_LABELS,
  BRIEF_QUESTIONS,
  STAGE_ARTIFACTS,
  STAGES,
  draft,
  useDraft,
  type BriefField,
} from "@/lib/store";
import { cn } from "@/lib/utils";

const FIELDS = Object.keys(BRIEF_LABELS) as BriefField[];

// The canvas shows one thing at a time: the artifact of the chosen stage.
// Stages with their artifacts run down the left, the next action sits at the bottom.
export function Canvas() {
  const d = useDraft();
  const [open, setOpen] = useState({ stage: 0, artifact: 0 });
  const filled = FIELDS.filter((f) => d.brief[f]).length;
  const missing = FIELDS.filter((f) => !d.brief[f]);
  const ready = missing.length === 0;

  return (
    <section className="flex min-h-0 flex-col overflow-hidden rounded-xl border bg-card shadow-panel">
      <header className="flex items-center gap-3 border-b px-5 py-3.5">
        <Boat size={28} bob />
        <div className="min-w-0">
          <h1 className="truncate text-[17px] font-semibold tracking-tight">{d.name}</h1>
          <p className="text-xs text-faint">команда SRE · владелец e.azimov · эпик появится после заявки</p>
        </div>
        <span className="ml-auto rounded-full bg-surface px-2.5 py-0.5 text-[11px] font-medium text-muted-foreground">
          {STAGES[d.stage]}
        </span>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-[208px_1fr]">
        <StageRail current={d.stage} open={open} onOpen={setOpen} />

        <div
          className="min-h-0 overflow-y-auto px-8 py-6"
          style={{
            background:
              "radial-gradient(90% 60% at 100% 0%, color-mix(in oklab, var(--primary) 9%, transparent), transparent 60%), var(--card)",
          }}
        >
          {open.stage === 0 ? (
            <Brief brief={d.brief} lastFilled={d.lastFilled} />
          ) : (
            <Later artifact={STAGE_ARTIFACTS[open.stage][open.artifact]} />
          )}
        </div>
      </div>

      <footer className="flex flex-wrap items-center gap-4 border-t bg-surface/60 px-5 py-3">
        <Progress value={filled} total={FIELDS.length} />
        <div className="min-w-0 flex-1 text-[12.5px]">
          {ready ? (
            <span className="text-muted-foreground">Заявка собрана. После подтверждения – эпик и базовые задачи в Jira.</span>
          ) : (
            <span className="text-faint">
              Не хватает: <span className="text-muted-foreground">{missing.map((f) => BRIEF_LABELS[f].toLowerCase()).join(", ")}</span>
            </span>
          )}
        </div>
        <button
          disabled={!ready || d.stage > 0}
          onClick={() => {
            draft.set((s) => ({ stage: s.stage + 1, events: [...s.events, "Эпик и базовые задачи созданы (пример)"] }));
            setOpen({ stage: 1, artifact: 0 });
          }}
          className="rounded-lg bg-primary px-4 py-2 text-[13px] font-semibold text-primary-foreground hover:bg-primary-hover disabled:opacity-40"
        >
          {d.stage > 0 ? "Заявка подтверждена" : "Подтвердить заявку"}
        </button>
      </footer>
    </section>
  );
}

function StageRail(props: {
  current: number;
  open: { stage: number; artifact: number };
  onOpen: (o: { stage: number; artifact: number }) => void;
}) {
  return (
    <nav className="min-h-0 overflow-y-auto border-r px-3 py-5">
      <ol className="relative">
        {STAGES.map((s, i) => {
          const done = i < props.current;
          const now = i === props.current;
          const locked = i > props.current;
          return (
            <li key={s} className="relative pb-5 pl-8 last:pb-0">
              {i < STAGES.length - 1 && (
                <span className={cn("absolute left-[9px] top-6 bottom-0 w-px", done ? "bg-success/60" : "bg-border-strong")} />
              )}
              <span
                className={cn(
                  "absolute left-0 top-0.5 grid size-5 place-items-center rounded-full border-[1.5px] border-border-strong text-[10px] text-faint",
                  done && "border-success bg-success text-background",
                  now && "border-primary bg-primary text-primary-foreground shadow-[0_0_0_4px_color-mix(in_oklab,var(--primary)_20%,transparent)]",
                )}
              >
                {done ? <Check size={11} /> : i + 1}
              </span>
              <div className={cn("text-[13px] font-semibold", locked ? "text-faint" : "text-foreground")}>{s}</div>
              <ul className="mt-1 space-y-0.5">
                {STAGE_ARTIFACTS[i].map((a, j) => {
                  const active = props.open.stage === i && props.open.artifact === j;
                  return (
                    <li key={a}>
                      <button
                        disabled={locked}
                        onClick={() => props.onOpen({ stage: i, artifact: j })}
                        className={cn(
                          "flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[12.5px] text-muted-foreground hover:bg-surface hover:text-foreground disabled:cursor-default disabled:text-faint disabled:hover:bg-transparent",
                          active && "bg-surface text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
                        )}
                      >
                        {locked && <Lock size={11} className="shrink-0 opacity-60" />}
                        {a}
                      </button>
                    </li>
                  );
                })}
              </ul>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function Brief(props: { brief: Partial<Record<BriefField, string>>; lastFilled?: BriefField }) {
  return (
    <article className="mx-auto max-w-2xl">
      <h2 className="text-lg font-semibold tracking-tight">Бриф</h2>
      <p className="mb-6 mt-1 text-[13px] text-faint">
        Что агент должен делать – своими словами. Билдер заполняет поля по твоим ответам в чате.
      </p>
      <div className="space-y-2.5">
        {FIELDS.map((f) => {
          const value = props.brief[f];
          return (
            <section
              key={f + (value ?? "")}
              className={cn(
                "rounded-xl border px-4 py-3 transition-colors",
                value ? "bg-card" : "border-dashed bg-transparent",
                props.lastFilled === f && value && "animate-[flash_1.6s_ease-out]",
              )}
            >
              <div className="mb-1 flex items-center gap-2 text-[12px] font-semibold text-muted-foreground">
                <span className={cn("size-1.5 rounded-full", value ? "bg-success" : "bg-border-strong")} />
                {BRIEF_LABELS[f]}
              </div>
              {value ? (
                <p className="text-[13.5px] text-foreground">{value}</p>
              ) : (
                <p className="text-[13px] italic text-faint">{BRIEF_QUESTIONS[f]}</p>
              )}
            </section>
          );
        })}
      </div>
    </article>
  );
}

function Later({ artifact }: { artifact: string }) {
  return (
    <div className="grid h-full place-items-center text-center">
      <div>
        <Boat size={40} />
        <p className="mt-3 text-sm font-semibold">{artifact}</p>
        <p className="mt-1 text-[13px] text-faint">Появится на этом этапе – билдер подготовит его вместе с тобой.</p>
      </div>
    </div>
  );
}

function Progress({ value, total }: { value: number; total: number }) {
  const r = 9;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-2 text-[12.5px] font-medium text-muted-foreground">
      <svg width="24" height="24" viewBox="0 0 24 24" className="-rotate-90">
        <circle cx="12" cy="12" r={r} fill="none" stroke="var(--border-strong)" strokeWidth="2.5" />
        <circle
          cx="12"
          cy="12"
          r={r}
          fill="none"
          stroke="var(--primary)"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - value / total)}
          style={{ transition: "stroke-dashoffset .5s" }}
        />
      </svg>
      {value} из {total}
    </div>
  );
}

import { ArrowRight, Check, CircleDashed, FileCode2, Lock, MessageSquare, Pencil } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Contract, PACKAGE } from "./Contract";
import { Boat } from "./Mascot";
import {
  BRIEF_LABELS,
  BRIEF_QUESTIONS,
  STAGE_ARTIFACTS,
  STAGES,
  CANVAS,
  draft,
  fillField,
  showRoom,
  useDraft,
  type BriefField,
  type Draft,
} from "@/lib/store";
import { cn } from "@/lib/utils";

const FIELDS = Object.keys(BRIEF_LABELS) as BriefField[];
const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

// The canvas shows one thing at a time: the artifact of the chosen stage.
// Stages with their artifacts run down the left, the next action sits at the bottom.
export function Canvas() {
  const d = useDraft();
  const [open, setOpen] = useState({ stage: 0, artifact: 0 });
  // Another artifact opens from its top; a focus request keeps its own scroll to the card.
  const content = useRef<HTMLDivElement>(null);
  const show = (o: { stage: number; artifact: number }) => {
    content.current?.scrollTo({ top: 0 });
    setOpen(o);
  };
  // The chat pointed at a brief field: the brief comes to the front.
  const focusN = d.focus?.n ?? 0;
  useEffect(() => {
    if (focusN) setOpen({ stage: 0, artifact: 0 });
  }, [focusN]);
  const filled = FIELDS.filter((f) => d.brief[f]).length;
  const missing = FIELDS.filter((f) => !d.brief[f]);
  const ready = missing.length === 0;

  return (
    <section className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-panel">
      <header className="flex items-center gap-3 border-b px-4 py-3 sm:px-5 sm:py-3.5">
        <Boat size={28} bob />
        <div className="min-w-0">
          <h1 className="truncate text-[22px] font-semibold tracking-tight">{d.name}</h1>
          <p className="truncate text-[13px] text-faint">команда SRE · владелец e.azimov · эпик появится после заявки</p>
        </div>
        <span className="ml-auto hidden shrink-0 rounded-full bg-surface sm:inline px-2.5 py-0.5 text-[12px] font-medium text-muted-foreground">
          {STAGES[d.stage]}
        </span>
      </header>

      <StageStrip current={d.stage} open={open} onOpen={show} />
      <div className="grid min-h-0 flex-1 md:grid-cols-[208px_minmax(0,1fr)]">
        <StageRail current={d.stage} open={open} onOpen={show} />

        <div
          ref={content}
          className="min-h-0 overflow-y-auto px-4 py-5 sm:px-8 sm:py-6"
          style={{
            background:
              "radial-gradient(90% 60% at 100% 0%, color-mix(in oklab, var(--primary) 9%, transparent), transparent 60%), var(--card)",
          }}
        >
          {open.stage === 0 ? (
            <Brief d={d} onPackage={() => show(PACKAGE)} />
          ) : open.stage === PACKAGE.stage && open.artifact === PACKAGE.artifact ? (
            <Contract d={d} />
          ) : (
            <Later artifact={STAGE_ARTIFACTS[open.stage][open.artifact]} />
          )}
        </div>
      </div>

      <footer className="flex flex-wrap items-center gap-x-4 gap-y-2.5 border-t bg-surface/60 px-4 py-3 sm:px-5">
        <Progress value={filled} total={FIELDS.length} />
        <div className="min-w-0 flex-1 basis-48 text-[13px]">
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
          className="w-full shrink-0 rounded-lg bg-primary px-4 py-2 sm:w-auto text-[14px] font-semibold text-primary-foreground hover:bg-primary-hover disabled:bg-surface disabled:text-muted-foreground disabled:shadow-[inset_0_0_0_1px_var(--border-strong)]"
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
    <nav className="hidden min-h-0 overflow-y-auto border-r px-3 py-5 md:block">
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
                  "absolute left-0 top-0.5 grid size-5 place-items-center rounded-full border-[1.5px] border-border-strong text-[11px] text-faint",
                  done && "border-success bg-success text-background",
                  now && "border-primary bg-primary text-primary-foreground shadow-[0_0_0_4px_color-mix(in_oklab,var(--primary)_20%,transparent)]",
                )}
              >
                {done ? <Check size={11} /> : i + 1}
              </span>
              <div className={cn("text-[14px] font-semibold", locked ? "text-faint" : "text-foreground")}>{s}</div>
              <ul className="mt-1 space-y-0.5">
                {STAGE_ARTIFACTS[i].map((a, j) => {
                  const active = props.open.stage === i && props.open.artifact === j;
                  const early = locked && isPackage(i, j);
                  return (
                    <li key={a}>
                      <button
                        disabled={locked && !early}
                        onClick={() => props.onOpen({ stage: i, artifact: j })}
                        className={cn(
                          "flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground disabled:cursor-default disabled:text-faint disabled:hover:bg-transparent",
                          active && "bg-surface text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
                        )}
                      >
                        {locked && !early && <Lock size={11} className="shrink-0 opacity-60" />}
                        <span className="truncate" title={early ? `${a} – черновик до заявки` : a}>
                          {a}
                        </span>
                        {early && <CircleDashed size={11} className="ml-auto shrink-0 text-faint" aria-label="черновик" />}
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

// Below md the rail turns into one scrolling row of artifacts, numbered by stage.
function StageStrip(props: {
  current: number;
  open: { stage: number; artifact: number };
  onOpen: (o: { stage: number; artifact: number }) => void;
}) {
  return (
    <nav aria-label="Этапы" className="flex shrink-0 gap-1.5 overflow-x-auto border-b px-3 py-2 md:hidden">
      {STAGE_ARTIFACTS.flatMap((artifacts, i) =>
        artifacts.map((a, j) => {
          const locked = i > props.current;
          const done = i < props.current;
          const active = props.open.stage === i && props.open.artifact === j;
          const early = locked && isPackage(i, j);
          return (
            <button
              key={a}
              disabled={locked && !early}
              aria-current={active ? "step" : undefined}
              onClick={() => props.onOpen({ stage: i, artifact: j })}
              className={cn(
                "flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-[13px] text-muted-foreground disabled:text-faint",
                active && "border-primary bg-surface text-foreground",
              )}
            >
              <span
                className={cn(
                  "grid size-4 place-items-center rounded-full text-[11px] tabular-nums",
                  done ? "bg-success text-background" : active ? "bg-primary text-primary-foreground" : "bg-surface",
                )}
              >
                {done ? <Check size={10} /> : i + 1}
              </span>
              {locked && !early && <Lock size={11} className="opacity-60" />}
              {a}
              {early && <DraftMark />}
            </button>
          );
        }),
      )}
    </nav>
  );
}

const isPackage = (stage: number, artifact: number) => stage === PACKAGE.stage && artifact === PACKAGE.artifact;

function DraftMark() {
  return (
    <span className="ml-auto shrink-0 rounded-full border border-dashed border-border-strong px-1.5 text-[11px] leading-4 text-faint">
      черновик
    </span>
  );
}

function Brief({ d, onPackage }: { d: Draft; onPackage: () => void }) {
  return (
    <article className="mx-auto max-w-2xl">
      <h2 className="text-lg font-semibold tracking-tight">Бриф</h2>
      <p className="mb-6 mt-1 text-[14px] text-faint">
        Что агент должен делать – своими словами. Билдер заполняет поля по твоим ответам в чате, поправить можно и тут.
      </p>
      <div className="space-y-2.5">
        {FIELDS.map((f) => (
          <BriefCard key={f} field={f} d={d} />
        ))}
      </div>
      <button
        type="button"
        onClick={onPackage}
        className="mt-6 flex items-center gap-1.5 rounded-md text-[14px] text-muted-foreground hover:text-foreground"
      >
        <FileCode2 size={14} aria-hidden />
        Черновик пакета
        <ArrowRight size={14} aria-hidden />
      </button>
    </article>
  );
}

function BriefCard({ field: f, d }: { field: BriefField; d: Draft }) {
  const value = d.brief[f];
  const source = d.sources[f];
  const room = source && d.rooms.find((r) => r.id === source.room);
  const ref = useRef<HTMLElement>(null);
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  // A flash marks a field the builder just filled or the chat pointed at; `pulse` restarts it.
  const [pulse, setPulse] = useState(0);

  const focused = d.focus?.field === f ? d.focus.n : 0;
  useEffect(() => {
    if (!focused) return;
    ref.current?.scrollIntoView({ behavior: "smooth", block: "center" });
    setPulse((n) => n + 1);
  }, [focused]);
  const filledNow = d.lastFilled === f;
  useEffect(() => {
    if (filledNow) setPulse((n) => n + 1);
  }, [filledNow, value]);

  const start = () => {
    setText(value ?? "");
    setEditing(true);
  };
  const save = () => {
    const v = text.trim();
    if (v && v !== value) fillField(f, v, CANVAS);
    setEditing(false);
  };

  return (
    <section
      ref={ref}
      key={pulse}
      onAnimationEnd={() => filledNow && draft.set({ lastFilled: undefined })}
      className={cn(
        "group rounded-xl border px-4 py-3",
        value || editing ? "bg-card" : "border-dashed bg-transparent",
        pulse > 0 && "animate-[flash_1.6s_ease-out]",
      )}
    >
      <div className="mb-1 flex items-center gap-2 text-[13px] font-semibold text-muted-foreground">
        <span className={cn("size-1.5 rounded-full", value ? "bg-success" : "bg-border-strong")} />
        {BRIEF_LABELS[f]}
        {!editing && (
          <button
            type="button"
            onClick={start}
            aria-label={value ? `Поправить «${BRIEF_LABELS[f]}»` : `Заполнить «${BRIEF_LABELS[f]}» вручную`}
            className="ml-auto grid size-7 place-items-center rounded-md text-faint hover:bg-surface hover:text-foreground focus-visible:opacity-100 sm:opacity-0 sm:group-hover:opacity-100"
          >
            <Pencil size={13} />
          </button>
        )}
      </div>
      {editing ? (
        <div>
          <textarea
            autoFocus
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setEditing(false);
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save();
            }}
            aria-label={BRIEF_LABELS[f]}
            placeholder={BRIEF_QUESTIONS[f]}
            rows={3}
            className="w-full resize-y rounded-lg border bg-surface px-3 py-2 text-[15px] outline-none placeholder:text-faint focus:border-primary"
          />
          <div className="mt-2 flex items-center gap-2">
            <button
              type="button"
              onClick={save}
              className="rounded-md bg-primary px-3 py-1 text-[13px] font-semibold text-primary-foreground hover:bg-primary-hover"
            >
              Сохранить
            </button>
            <button
              type="button"
              onClick={() => setEditing(false)}
              className="rounded-md px-3 py-1 text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground"
            >
              Отмена
            </button>
            <span className="ml-auto hidden text-[12px] text-faint sm:inline">{isMac ? "⌘" : "Ctrl"}+Enter · Esc</span>
          </div>
        </div>
      ) : value ? (
        <p className="text-[15px] text-foreground [overflow-wrap:anywhere]">{value}</p>
      ) : (
        <p className="text-[14px] italic text-faint">{BRIEF_QUESTIONS[f]}</p>
      )}
      {source && !editing && (
        <div className="mt-2 flex items-center gap-1.5 text-[12px] text-faint">
          {room ? (
            <button
              type="button"
              onClick={() => showRoom(room.id)}
              className="flex min-w-0 items-center gap-1 rounded hover:text-foreground"
              title="Открыть беседу"
            >
              <MessageSquare size={12} aria-hidden className="shrink-0" />
              <span className="truncate">из беседы «{room.title}»</span>
            </button>
          ) : (
            <span>поправлено вручную</span>
          )}
          <span aria-hidden>·</span>
          <time className="tabular-nums" dateTime={new Date(source.at).toISOString()}>
            {new Date(source.at).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })}
          </time>
        </div>
      )}
    </section>
  );
}

function Later({ artifact }: { artifact: string }) {
  return (
    <div className="grid h-full place-items-center text-center">
      <div>
        <Boat size={40} />
        <p className="mt-3 text-[15px] font-semibold">{artifact}</p>
        <p className="mt-1 text-[14px] text-faint">Появится на этом этапе – билдер подготовит его вместе с тобой.</p>
      </div>
    </div>
  );
}

function Progress({ value, total }: { value: number; total: number }) {
  const r = 9;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-2 text-[13px] font-medium tabular-nums text-muted-foreground">
      <svg width="24" height="24" viewBox="0 0 24 24" className="-rotate-90" aria-hidden>
        <circle cx="12" cy="12" r={r} fill="none" stroke="var(--border-strong)" strokeWidth="2.5" />
        <circle
          cx="12"
          cy="12"
          r={r}
          fill="none"
          stroke={value === total ? "var(--success)" : "var(--primary)"}
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - value / total)}
          style={{ transition: "stroke-dashoffset .5s, stroke .3s" }}
        />
      </svg>
      {value} из {total}
    </div>
  );
}

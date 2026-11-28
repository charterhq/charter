import { ArrowRight, Check, CircleDashed, FileCode2, Lock, MessageSquare, Pencil } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Contract } from "./Contract";
import { Boat } from "./Mascot";
import type { Field, Stage } from "@/lib/api";
import { counted, draft, editField, label, partTitles, question, rename, showRoom, useDraft, type Draft } from "@/lib/store";
import { cn } from "@/lib/utils";

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

// The canvas shows one thing at a time: the artifact of the chosen stage.
// Stages with their artifacts run down the left, the next action sits at the bottom.
type Open = { stage: number; artifact: number };

const kindAt = (stages: Stage[], o: Open) => stages[o.stage]?.artifacts?.[o.artifact]?.kind ?? "";
const titleAt = (stages: Stage[], o: Open) => stages[o.stage]?.artifacts?.[o.artifact]?.title ?? "";
// The package opens as a read-only draft before its stage: what the brief already says.
const early = (stages: Stage[], o: Open, current: number) => o.stage > current && kindAt(stages, o) === "package";
const packageAt = (stages: Stage[]): Open => {
  for (let i = 0; i < stages.length; i++) {
    const j = stages[i].artifacts?.findIndex((a) => a.kind === "package") ?? -1;
    if (j >= 0) return { stage: i, artifact: j };
  }
  return { stage: 1, artifact: 0 };
};

// The canvas shows one thing at a time: the artifact of the chosen stage.
// Stages with their artifacts run down the left, the next action sits at the bottom.
export function Canvas() {
  const d = useDraft();
  const [open, setOpen] = useState<Open>({ stage: 0, artifact: 0 });
  // Another artifact opens from its top; a focus request keeps its own scroll to the card.
  const content = useRef<HTMLDivElement>(null);
  const show = (o: Open) => {
    content.current?.scrollTo({ top: 0 });
    setOpen(o);
  };
  // The chat pointed at a brief field: the brief comes to the front.
  const focusN = d.focus?.n ?? 0;
  useEffect(() => {
    if (focusN) setOpen({ stage: 0, artifact: 0 });
  }, [focusN]);
  const total = counted(d).length;
  const filled = total - d.missing.length;
  const owners = d.members.filter((m) => m.role === "owner").map((m) => m.person);
  const team = typeof d.attrs.team === "string" ? d.attrs.team : "";
  const kind = kindAt(d.stages, open);

  return (
    <section className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-panel">
      <header className="flex items-center gap-3 border-b px-4 py-3 sm:px-5 sm:py-3.5">
        <Boat size={28} bob />
        <div className="min-w-0">
          <Title name={d.name} />
          <p className="truncate text-[13px] text-faint">
            {[team && `команда ${team}`, owners.length && `владелец ${owners.join(", ")}`, "эпик появится после заявки"].filter(Boolean).join(" · ")}
          </p>
        </div>
        <span className="ml-auto hidden shrink-0 rounded-full bg-surface sm:inline px-2.5 py-0.5 text-[12px] font-medium text-muted-foreground">
          {d.stages[d.stage]?.title}
        </span>
      </header>

      <StageStrip d={d} open={open} onOpen={show} />
      <div className="grid min-h-0 flex-1 md:grid-cols-[208px_minmax(0,1fr)]">
        <StageRail d={d} open={open} onOpen={show} />

        <div
          ref={content}
          className="min-h-0 overflow-y-auto px-4 py-5 sm:px-8 sm:py-6"
          style={{
            background:
              "radial-gradient(90% 60% at 100% 0%, color-mix(in oklab, var(--gradient-from) 45%, transparent), transparent 60%), var(--card)",
          }}
        >
          {kind === "brief" || (open.stage === 0 && open.artifact === 0 && !kind) ? (
            <Brief d={d} onPackage={() => show(packageAt(d.stages))} />
          ) : kind === "package" ? (
            <Contract d={d} draftOnly={open.stage > d.stage} />
          ) : (
            <Later artifact={titleAt(d.stages, open)} />
          )}
        </div>
      </div>

      <footer className="flex flex-wrap items-center gap-x-4 gap-y-2.5 border-t bg-surface/60 px-4 py-3 sm:px-5">
        <Progress value={filled} total={total} />
        <div className="min-w-0 flex-1 basis-48 text-[13px]">
          {d.missing.length === 0 ? (
            <span className="text-muted-foreground">Заявка собрана. После подтверждения – эпик и базовые задачи в Jira.</span>
          ) : (
            <span className="text-faint">
              Не хватает: <span className="text-muted-foreground">{d.missing.map((f) => label(d, f).toLowerCase()).join(", ")}</span>
            </span>
          )}
        </div>
        <button
          disabled
          title="Подтверждение заявки появится, когда ядро научится переходам стадий"
          className="w-full shrink-0 rounded-lg bg-primary px-4 py-2 sm:w-auto text-[14px] font-semibold text-primary-foreground hover:bg-primary-hover disabled:bg-surface disabled:text-muted-foreground disabled:shadow-[inset_0_0_0_1px_var(--border-strong)]"
        >
          Подтвердить заявку
        </button>
      </footer>
    </section>
  );
}

// The name of the draft: click to rename.
function Title({ name }: { name: string }) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(name);
  if (editing)
    return (
      <input
        autoFocus
        value={text}
        aria-label="Имя агента"
        onChange={(e) => setText(e.target.value)}
        onBlur={() => {
          setEditing(false);
          if (text.trim() && text.trim() !== name) void rename(text.trim());
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          if (e.key === "Escape") {
            setText(name);
            setEditing(false);
          }
        }}
        className="w-full rounded-md border bg-surface px-1.5 text-[22px] font-semibold tracking-tight outline-none focus:border-primary"
      />
    );
  return (
    <h1 className="truncate text-[22px] font-semibold tracking-tight">
      <button
        type="button"
        onClick={() => {
          setText(name);
          setEditing(true);
        }}
        title="Переименовать"
        className="max-w-full truncate rounded-md text-left hover:bg-surface"
      >
        {name}
      </button>
    </h1>
  );
}

function StageRail({ d, open, onOpen }: { d: Draft; open: Open; onOpen: (o: Open) => void }) {
  return (
    <nav className="hidden min-h-0 overflow-y-auto border-r px-3 py-5 md:block">
      <ol className="relative">
        {d.stages.map((s, i) => {
          const done = i < d.stage;
          const now = i === d.stage;
          const locked = i > d.stage;
          return (
            <li key={s.id} className="relative pb-5 pl-8 last:pb-0">
              {i < d.stages.length - 1 && (
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
              <div className={cn("text-[14px] font-semibold", locked ? "text-faint" : "text-foreground")}>{s.title}</div>
              <ul className="mt-1 space-y-0.5">
                {(s.artifacts ?? []).map((a, j) => {
                  const o = { stage: i, artifact: j };
                  const active = open.stage === i && open.artifact === j;
                  const draftOnly = early(d.stages, o, d.stage);
                  return (
                    <li key={a.kind + j}>
                      <button
                        disabled={locked && !draftOnly}
                        onClick={() => onOpen(o)}
                        className={cn(
                          "flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-[13px] text-muted-foreground hover:bg-surface hover:text-foreground disabled:cursor-default disabled:text-faint disabled:hover:bg-transparent",
                          active && "bg-surface text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
                        )}
                      >
                        {locked && !draftOnly && <Lock size={11} className="shrink-0 opacity-60" />}
                        <span className="truncate" title={draftOnly ? `${a.title} – черновик до заявки` : a.title}>
                          {a.title}
                        </span>
                        {draftOnly && <CircleDashed size={11} className="ml-auto shrink-0 text-faint" aria-label="черновик" />}
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
function StageStrip({ d, open, onOpen }: { d: Draft; open: Open; onOpen: (o: Open) => void }) {
  return (
    <nav aria-label="Этапы" className="flex shrink-0 gap-1.5 overflow-x-auto border-b px-3 py-2 md:hidden">
      {d.stages.flatMap((s, i) =>
        (s.artifacts ?? []).map((a, j) => {
          const o = { stage: i, artifact: j };
          const locked = i > d.stage;
          const done = i < d.stage;
          const active = open.stage === i && open.artifact === j;
          const draftOnly = early(d.stages, o, d.stage);
          return (
            <button
              key={s.id + a.kind + j}
              disabled={locked && !draftOnly}
              aria-current={active ? "step" : undefined}
              onClick={() => onOpen(o)}
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
              {locked && !draftOnly && <Lock size={11} className="opacity-60" />}
              {a.title}
              {draftOnly && <DraftMark />}
            </button>
          );
        }),
      )}
    </nav>
  );
}

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
        {d.fields
          .filter((f) => !f.readonly_model)
          .map((f) => (
            <BriefCard key={f.name} field={f} d={d} />
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

const isEmpty = (v: unknown): boolean =>
  v == null ||
  (typeof v === "string" && !v.trim()) ||
  (Array.isArray(v) && v.length === 0) ||
  (typeof v === "object" && !Array.isArray(v) && Object.values(v as object).every(isEmpty));

// A value of an object field or a collection, with the titles of its parts from the schema.
function Value({ d, field, value }: { d: Draft; field: Field; value: unknown }) {
  const titles = partTitles(d, field.name);
  const parts = (o: Record<string, unknown>) =>
    Object.entries(o)
      .filter(([k, v]) => k !== "id" && !isEmpty(v))
      .map(([k, v]) => (
        <div key={k}>
          <span className="text-muted-foreground">{titles[k] ?? k}:</span> {typeof v === "string" ? v : JSON.stringify(v)}
        </div>
      ));
  if (typeof value === "string") return <p className="whitespace-pre-wrap text-[15px] text-foreground [overflow-wrap:anywhere]">{value}</p>;
  if (Array.isArray(value))
    return (
      <ol className="list-decimal space-y-1.5 pl-5 text-[15px] [overflow-wrap:anywhere]">
        {value.map((it, i) => (
          <li key={(it as { id?: string })?.id ?? i}>{it && typeof it === "object" ? parts(it as Record<string, unknown>) : String(it)}</li>
        ))}
      </ol>
    );
  if (value && typeof value === "object") return <div className="space-y-0.5 text-[15px] [overflow-wrap:anywhere]">{parts(value as Record<string, unknown>)}</div>;
  return <p className="text-[15px]">{String(value)}</p>;
}

function BriefCard({ field: f, d }: { field: Field; d: Draft }) {
  const value = d.brief[f.name];
  const filled = !isEmpty(value);
  // A collection or a field with parts has a source per item; the card shows the latest.
  const source = Object.entries(d.sources)
    .filter(([p]) => p === f.name || p.startsWith(f.name + "/"))
    .map(([, s]) => s)
    .sort((a, b) => b.rev - a.rev)[0];
  const room = source?.conversation_id ? d.rooms.find((r) => r.id === source.conversation_id) : undefined;
  const editable = f.type === "string" && !f.collection;
  const ref = useRef<HTMLElement>(null);
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  // A flash marks a field the builder just filled or the chat pointed at; `pulse` restarts it.
  const [pulse, setPulse] = useState(0);

  const focused = d.focus?.field === f.name ? d.focus.n : 0;
  useEffect(() => {
    if (!focused) return;
    ref.current?.scrollIntoView({ behavior: "smooth", block: "center" });
    setPulse((n) => n + 1);
  }, [focused]);
  const filledNow = d.lastFilled === f.name;
  useEffect(() => {
    if (filledNow) setPulse((n) => n + 1);
  }, [filledNow, value]);

  const start = () => {
    setText(typeof value === "string" ? value : "");
    setEditing(true);
  };
  const save = () => {
    const v = text.trim();
    if (v !== (value ?? "")) void editField(f.name, v || null);
    setEditing(false);
  };
  const name = label(d, f.name);
  const ask = question(d, f.name);

  return (
    <section
      ref={ref}
      key={pulse}
      onAnimationEnd={() => filledNow && draft.set({ lastFilled: undefined })}
      className={cn(
        "group rounded-xl border px-4 py-3",
        filled || editing ? "bg-card" : "border-dashed bg-transparent",
        pulse > 0 && "animate-[flash_1.6s_ease-out]",
      )}
    >
      <div className="mb-1 flex items-center gap-2 text-[13px] font-semibold text-muted-foreground">
        <span className={cn("size-1.5 rounded-full", filled ? "bg-success" : "bg-border-strong")} />
        {name}
        {!editing && editable && (
          <button
            type="button"
            onClick={start}
            aria-label={filled ? `Поправить "${name}"` : `Заполнить "${name}" вручную`}
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
            aria-label={name}
            placeholder={ask}
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
      ) : filled ? (
        <Value d={d} field={f} value={value} />
      ) : (
        <p className="text-[14px] italic text-faint">
          {ask || "Пока пусто"}
          {!editable && <span className="not-italic"> · заполняется в беседе</span>}
        </p>
      )}
      {source && !editing && (
        <div className="mt-2 flex items-center gap-1.5 text-[12px] text-faint">
          {source.source === "conversation" ? (
            <button
              type="button"
              onClick={() => room && showRoom(room.id)}
              disabled={!room}
              className="flex min-w-0 items-center gap-1 rounded hover:text-foreground disabled:hover:text-faint"
              title="Открыть беседу"
            >
              <MessageSquare size={12} aria-hidden className="shrink-0" />
              <span className="truncate">{room ? `из беседы "${room.title || "без названия"}"` : "из беседы"}</span>
            </button>
          ) : (
            <span>поправил {source.by}</span>
          )}
          <span aria-hidden>·</span>
          <time className="tabular-nums" dateTime={source.at}>
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
          stroke={total && value === total ? "var(--success)" : "var(--primary)"}
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - (total ? value / total : 0))}
          style={{ transition: "stroke-dashoffset .5s, stroke .3s" }}
        />
      </svg>
      {value} из {total}
    </div>
  );
}

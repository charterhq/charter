import { Check } from "lucide-react";
import { Boat } from "./Mascot";
import { BRIEF_LABELS, STAGES, draft, useDraft, type BriefField } from "@/lib/store";
import { cn } from "@/lib/utils";

const FIELDS = Object.keys(BRIEF_LABELS) as BriefField[];

export function Canvas() {
  const d = useDraft();
  const filled = FIELDS.filter((f) => d.brief[f]).length;
  const ready = filled === FIELDS.length;

  return (
    <section
      className="relative min-h-0 overflow-y-auto rounded-xl border p-6 shadow-panel"
      style={{
        background:
          "radial-gradient(120% 70% at 100% 0%, color-mix(in oklab, var(--primary) 12%, transparent), transparent 60%), radial-gradient(var(--border) 1px, transparent 1px) 0 0/18px 18px, var(--card)",
      }}
    >
      <div className="absolute right-5 top-4">
        <Boat size={56} bob />
      </div>
      <h1 className="text-xl font-semibold tracking-tight">{d.name}</h1>
      <p className="mb-5 mt-0.5 text-[12.5px] text-faint">черновик · эпик появится после заявки</p>

      <Stages current={d.stage} />

      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
        <Card title="Заявка" tag={ready ? "готова" : "черновик"} tone={ready ? "ok" : "wip"}>
          <ul className="space-y-1.5">
            {FIELDS.map((f) => (
              <li key={f} className="text-[12.5px]">
                <span className="text-faint">{BRIEF_LABELS[f]}: </span>
                <span className={d.brief[f] ? "text-muted-foreground" : "text-faint italic"}>{d.brief[f] ?? "ждет ответа"}</span>
              </li>
            ))}
          </ul>
        </Card>
        <Card title="Путь агента" tag="набросок" tone="todo">
          <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            {["событие", "проверки", "вывод", "ответ"].map((n, i, a) => (
              <span key={n} className="flex items-center gap-1.5">
                <span className="rounded-md border border-border-strong bg-surface px-2 py-0.5">{n}</span>
                {i < a.length - 1 && <span className="text-primary">→</span>}
              </span>
            ))}
          </div>
        </Card>
        <Card title="Готовность" tag={`${filled} из ${FIELDS.length}`} tone={ready ? "ok" : "todo"}>
          <p className="text-[12.5px] text-muted-foreground">
            {ready ? "Заявку можно подтверждать" : "Отвечай билдеру – поля заполнятся сами"}
          </p>
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-surface">
            <div
              className="h-full rounded-full bg-gradient-to-r from-primary-hover to-sky transition-[width] duration-500"
              style={{ width: `${(filled / FIELDS.length) * 100}%` }}
            />
          </div>
        </Card>
      </div>

      {d.events.length > 0 && (
        <ul className="mt-4 space-y-1 text-xs text-muted-foreground">
          {d.events.map((e) => (
            <li key={e} className="flex items-center gap-1.5">
              <Check size={13} className="text-success" />
              {e}
            </li>
          ))}
        </ul>
      )}

      <div className="mt-5 flex flex-wrap items-center gap-2.5">
        <button
          disabled={!ready || d.stage > 0}
          onClick={() =>
            draft.set((s) => ({ stage: s.stage + 1, events: [...s.events, "Эпик и базовые задачи созданы (пример)"] }))
          }
          className="rounded-lg bg-primary px-4 py-2 text-[13px] font-semibold text-primary-foreground hover:bg-primary-hover disabled:opacity-40"
        >
          Подтвердить этап
        </button>
        <span className="text-xs text-faint">после подтверждения – эпик и базовые задачи в Jira</span>
      </div>
    </section>
  );
}

function Stages({ current }: { current: number }) {
  return (
    <ol className="mb-6 flex flex-wrap items-center gap-y-2">
      {STAGES.map((s, i) => (
        <li key={s} className="flex items-center">
          <span
            className={cn(
              "flex items-center gap-2 text-[12.5px] font-medium text-faint",
              i < current && "text-muted-foreground",
              i === current && "text-foreground",
            )}
          >
            <span
              className={cn(
                "grid size-5 place-items-center rounded-full border-[1.5px] border-border-strong text-[10px]",
                i < current && "border-success bg-success text-background",
                i === current &&
                  "border-primary bg-primary text-primary-foreground shadow-[0_0_0_4px_color-mix(in_oklab,var(--primary)_22%,transparent)]",
              )}
            >
              {i < current ? <Check size={11} /> : i + 1}
            </span>
            {s}
          </span>
          {i < STAGES.length - 1 && <span className="mx-2 h-px w-7 bg-border-strong" />}
        </li>
      ))}
    </ol>
  );
}

const TONES = {
  ok: "text-success bg-[color-mix(in_oklab,var(--success)_14%,transparent)]",
  wip: "text-warning bg-[color-mix(in_oklab,var(--warning)_14%,transparent)]",
  todo: "text-muted-foreground bg-surface",
};

function Card(props: { title: string; tag: string; tone: keyof typeof TONES; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border bg-[color-mix(in_oklab,var(--card)_85%,var(--background))] p-3.5">
      <div className="mb-2 flex items-center justify-between text-[13px] font-semibold">
        {props.title}
        <span className={cn("rounded-full px-2 text-[11px] font-medium", TONES[props.tone])}>{props.tag}</span>
      </div>
      {props.children}
    </div>
  );
}

import { Brain, FilePen, FilePlus, FileText, Globe, GitBranch, MessageSquare, Play, Search, Sparkles } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "./arc/button/button";
import { AgentPlan, type PlanActivity, type PlanStep } from "./blocks/AgentPlan";
import { AgentRun, RunOutput, type RunState, type RunStep } from "./blocks/AgentRun";
import { AiAnswer, AnswerCode, AnswerTable, Claim, Cite, type AnswerSource, type Feedback } from "./blocks/AiAnswer";
import { AiComposer, ToolToggle } from "./blocks/AiComposer";
import { ResearchRun, type ResearchQuery, type ResearchSource } from "./blocks/ResearchRun";
import { ToolCall, ToolCallGroup } from "./blocks/ToolCall";
import { Boat } from "./Mascot";

// Chat blocks on sample data for ?kit. The live UI shows the run and the brief writes; the rest waits
// for the core to report steps, tool calls, plans and sources.

const RUN: Omit<RunStep, "status">[] = [
  {
    id: "plan",
    icon: <Sparkles size={13} />,
    title: "Разобрал алерт и выбрал первый шаг",
    meta: "1.2s",
  },
  {
    id: "grafana",
    icon: <Search size={13} />,
    title: "Нашел панель в Grafana: api-gateway / 5xx",
    meta: "0.9s",
    details: <RunOutput call="grafana.search api-gateway 5xx">{"api-gateway / 5xx by route\napi-gateway / latency p99"}</RunOutput>,
  },
  {
    id: "logs",
    icon: <FileText size={13} />,
    title: "Прочитал логи за 15 минут",
    meta: "2.4s",
    details: (
      <RunOutput call="logs.query service=api-gateway since=15m">
        {"12:04:11 upstream tls: certificate has expired\n12:04:11 upstream tls: certificate has expired\n… 418 строк"}
      </RunOutput>
    ),
  },
  {
    id: "post",
    icon: <MessageSquare size={13} />,
    title: "Написал дежурному в #alerts",
    meta: "0.6s",
    details: <RunOutput call="mattermost.post #alerts" />,
  },
];

// play walks a list of steps once on a button press; nothing runs on its own.
// What each plan step does, line by line, as the demo runs it.
const PLAN_ACTIVITY: PlanActivity[][] = [
  [
    { icon: <Search size={12} />, text: "Нашел канал #alerts" },
    { icon: <FilePen size={12} />, text: "Подписался на канал" },
  ],
  [
    { icon: <Search size={12} />, text: "Искал панели по api-gateway" },
    { icon: <FileText size={12} />, text: "Прочитал правило алерта" },
    { icon: <Globe size={12} />, text: "Нашел панель api-gateway / 5xx" },
  ],
  [
    { icon: <FileText size={12} />, text: "Прочитал логи за 15 минут" },
    { icon: <Search size={12} />, text: "Искал похожие инциденты" },
    { icon: <Play size={12} />, text: "Сверил с runbook" },
  ],
  [
    { icon: <FilePlus size={12} />, text: "Написал черновик ответа" },
    { icon: <MessageSquare size={12} />, text: "Отправил дежурному" },
  ],
];

const PLAN_FAIL = 2;
const PLAN_RETRY: PlanActivity[] = [
  { icon: <Play size={12} />, text: "Повторил запрос в Loki" },
  { icon: <FileText size={12} />, text: "Прочитал логи за 15 минут" },
];

function usePlay(count: number, ms: number) {
  const [at, setAt] = useState(count);
  useEffect(() => {
    if (at >= count) return;
    const id = window.setTimeout(() => setAt(at + 1), ms);
    return () => window.clearTimeout(id);
  }, [at, count, ms]);
  return [at, () => setAt(0)] as const;
}

const SOURCES: AnswerSource[] = [
  {
    id: "s1",
    title: "Runbook: 5xx на api-gateway",
    site: "wiki",
    mark: "W",
    publisher: "Вики SRE",
    snippet: "Сначала срок сертификата на балансировщике, потом логи за 15 минут.",
    date: "12.03.2026",
  },
  {
    id: "s2",
    title: "Инцидент 14.10: истек сертификат",
    site: "jira",
    mark: "J",
    publisher: "Jira",
    snippet: "5xx шли 40 минут, пока не продлили сертификат.",
    date: "14.10.2025",
  },
  {
    id: "s3",
    title: "Дашборд api-gateway",
    site: "grafana",
    mark: "G",
    publisher: "Grafana",
    snippet: "Рост 5xx совпал с выкладкой в 12:04.",
    date: "28.11.2026",
  },
];

const QUERIES: Omit<ResearchQuery, "status">[] = [
  {
    id: "q1",
    text: "runbook 5xx api-gateway",
    results: 4,
    marks: [<FileText key="w" size={12} />, <Globe key="g" size={12} />],
  },
  {
    id: "q2",
    text: "инциденты api-gateway за месяц",
    results: 7,
    marks: [<GitBranch key="j" size={12} />],
  },
  {
    id: "q3",
    text: "панели ошибок api-gateway",
    results: 2,
    marks: [<Globe key="g" size={12} />, <Search key="s" size={12} />],
  },
];

const READING: Omit<ResearchSource, "progress">[] = [
  {
    ...SOURCES[0],
    year: "2025",
    snippet: "Сначала проверить срок сертификата на балансировщике, потом логи за 15 минут.",
    href: "https://wiki.example.com/runbooks/api-gateway-5xx",
  },
  {
    ...SOURCES[1],
    year: "2026",
    snippet: "14.10 истек сертификат на балансировщике, 5xx шли 40 минут.",
    href: "https://jira.example.com/browse/INC-1410",
  },
  {
    ...SOURCES[2],
    year: "2026",
    snippet: "Рост 5xx совпал с выкладкой в 12:04.",
    href: "https://grafana.example.com/d/api-gateway",
  },
];

export function KitBlocks({ Section }: { Section: (p: { title: string; children: ReactNode }) => ReactNode }) {
  const [runAt, replayRun] = usePlay(RUN.length, 900);
  const runState: RunState = runAt < RUN.length ? "working" : "done";
  const runSteps: RunStep[] = RUN.slice(0, Math.min(runAt + 1, RUN.length)).map((s, i) => ({ ...s, status: i < runAt ? "done" : "running" }));

  const [plan, setPlan] = useState<PlanStep[]>([
    { id: "a", title: "Подписаться на канал #alerts", status: "todo" },
    { id: "b", title: "Найти панель алерта в Grafana", status: "todo" },
    { id: "c", title: "Прочитать логи сервиса за 15 минут", status: "todo" },
    { id: "d", title: "Предложить дежурному первый шаг", status: "todo" },
  ]);
  const [approved, setApproved] = useState(false);
  const [planPaused, setPlanPaused] = useState(false);
  // The demo run: lines of the running step come one by one, the first a bit sooner; the third step
  // fails once, Retry runs it again with two more lines, Skip moves on.
  const [run, setRun] = useState({ at: 0, line: 0, failed: false, retried: false, skipped: [] as number[] });
  const runLines = (i: number) => [...PLAN_ACTIVITY[i % PLAN_ACTIVITY.length], ...(i === PLAN_FAIL && run.retried ? PLAN_RETRY : [])];
  useEffect(() => {
    if (!approved || planPaused || run.failed || run.at >= plan.length) return;
    const lines = runLines(run.at);
    const [ms, next] =
      run.line < lines.length
        ? [run.line === 0 ? 360 : 540, { ...run, line: run.line + 1 }]
        : run.at === PLAN_FAIL && !run.retried
          ? [520, { ...run, failed: true }]
          : [480, { ...run, at: run.at + 1, line: 0 }];
    const id = window.setTimeout(() => setRun(next), ms);
    return () => window.clearTimeout(id);
  });
  const replayPlan = () => setRun({ at: 0, line: 0, failed: false, retried: false, skipped: [] });
  const live: PlanStep[] = plan.map((s, i) => ({
    ...s,
    status: i < run.at ? (run.skipped.includes(i) ? "skipped" : "done") : i === run.at ? (run.failed ? "error" : "running") : "todo",
    meta: i < run.at ? `${(i * 7 + 12) % 40}s` : undefined,
    note:
      i === 2 ? (
        <>
          <Boat size={12} /> Добавил агент
        </>
      ) : undefined,
    activity: i <= run.at ? runLines(i).slice(0, i === run.at ? run.line : undefined) : undefined,
    error: i === PLAN_FAIL ? "Loki не ответил за 30 с: логи за 15 минут не получены" : undefined,
  }));

  const [qAt, replayResearch] = usePlay(QUERIES.length + 1, 1000);
  const researching = qAt <= QUERIES.length;

  const [callAllowed, setCallAllowed] = useState<"done" | "skipped" | undefined>();
  const [callFailed, setCallFailed] = useState(true);
  useEffect(() => {
    if (callFailed) return;
    const id = window.setTimeout(() => setCallFailed(true), 1500);
    return () => window.clearTimeout(id);
  }, [callFailed]);
  const [feedback, setFeedback] = useState<Feedback | undefined>();
  const [text, setText] = useState("");
  const [running, setRunning] = useState(false);
  const [web, setWeb] = useState(true);
  const [think, setThink] = useState(false);
  useEffect(() => {
    if (!running) return;
    const id = window.setTimeout(() => setRunning(false), 3000);
    return () => window.clearTimeout(id);
  }, [running]);

  return (
    <>
      <Section title="Блоки: tool-call">
        <div className="w-full max-w-xl space-y-3">
          <ToolCallGroup
            title="Использовал инструменты: 4"
            failed={callFailed ? 1 : 0}
            icons={[<Search key="s" size={11} />, <Globe key="g" size={11} />, <FileText key="f" size={11} />]}
          >
            <ToolCall
              icon={<Search size={13} />}
              title="Искал runbook по api-gateway"
              meta="4 результата · 1.2s"
              status="done"
              input={{ query: "runbook api-gateway 5xx", space: "SRE" }}
              inputText={'{ "query": "runbook api-gateway 5xx", "space": "SRE" }'}
              output={
                <ul className="space-y-1">
                  <li>Runbook: 5xx на api-gateway</li>
                  <li>Runbook: истекший сертификат</li>
                  <li>Как откатить выкладку</li>
                  <li>Дежурства SRE</li>
                </ul>
              }
              outputText="Runbook: 5xx на api-gateway"
            />
            <ToolCall
              icon={<Globe size={13} />}
              title="Открыл дашборд api-gateway"
              meta="760ms"
              status="done"
              input={{ dashboard: "api-gateway", range: "15m" }}
            />
            <ToolCall
              icon={<MessageSquare size={13} />}
              title="Написать в #alerts"
              status={callAllowed ?? "waiting"}
              message={callAllowed ? undefined : "Ждет подтверждения дежурного"}
              input={{
                channel: "#alerts",
                text: "Похоже на истекший сертификат",
              }}
              onAllow={() => setCallAllowed("done")}
              onDeny={() => setCallAllowed("skipped")}
            />
            <ToolCall
              icon={<GitBranch size={13} />}
              title="Открыть MR с правкой"
              status={callFailed ? "error" : "running"}
              message={callFailed ? "Нет прав на репозиторий" : undefined}
              input={{ repo: "platform/api-gateway", branch: "fix/cert" }}
              onRetry={() => setCallFailed(false)}
            />
          </ToolCallGroup>
        </div>
      </Section>

      <Section title="Блоки: agent-run">
        <div className="w-full max-w-xl space-y-3">
          <AgentRun
            title="Разбор алерта: 5xx на api-gateway"
            agent={
              <>
                <Boat size={16} /> alert-triage
              </>
            }
            state={runState}
            elapsed={runState === "done" ? "5.1s" : undefined}
            steps={runSteps}
            onRetry={replayRun}
            result={runState === "done" ? "Первый шаг: проверить срок сертификата, похоже на инцидент 14.10." : undefined}
            actions={
              runState === "done" ? (
                <Button size="sm" variant="secondary">
                  Открыть прогон
                </Button>
              ) : undefined
            }
          />
          <AgentRun
            title="Ответ в беседе"
            agent="builder"
            state="failed"
            steps={[{ id: "x", title: "Модель не ответила за 60 с", status: "error" }]}
            onRetry={() => undefined}
          />
        </div>
      </Section>

      <Section title="Блоки: agent-plan">
        <div className="w-full max-w-xl space-y-3">
          {!approved ? (
            <AgentPlan
              goal="Разбирать алерты Grafana и подсказывать дежурному"
              author={
                <>
                  <Brain size={13} /> reasoning-light
                </>
              }
              draft
              steps={plan}
              onChange={setPlan}
              onKeepPlanning={() => undefined}
              onApprove={() => {
                replayPlan();
                setApproved(true);
              }}
            />
          ) : (
            <AgentPlan
              goal="Разбирать алерты Grafana и подсказывать дежурному"
              steps={live}
              paused={planPaused}
              onPause={() => setPlanPaused(true)}
              onResume={() => setPlanPaused(false)}
              onRetryStep={() => setRun({ ...run, failed: false, retried: true })}
              onSkipStep={() => setRun({ ...run, failed: false, at: run.at + 1, line: 0, skipped: [...run.skipped, run.at] })}
              elapsed="1m 12s"
              summary="Алерт api-gateway разобран: сертификат истек 14 минут назад, дежурному ушел первый шаг из runbook и ссылка на панель."
              onStartOver={() => setApproved(false)}
            />
          )}
          {approved && (
            <Button size="sm" variant="ghost" onClick={() => setApproved(false)}>
              Вернуть черновик
            </Button>
          )}
        </div>
      </Section>

      <Section title="Блоки: research-run">
        <div className="w-full max-w-2xl">
          <ResearchRun
            question="Почему api-gateway отдает 5xx после выкладки?"
            running={researching}
            elapsed={researching ? `${qAt}s` : "4s"}
            queries={QUERIES.slice(0, Math.min(qAt + 1, QUERIES.length)).map((q, i) => ({ ...q, status: i < qAt ? "done" : "running" }))}
            sources={READING.slice(0, Math.min(qAt + 1, READING.length)).map((s, i) => ({ ...s, progress: i < qAt ? 1 : 0.35 }))}
            answer={researching ? undefined : <p>Похоже на истекший сертификат: так уже было 14.10, runbook советует сначала проверить его срок.</p>}
            onStop={() => undefined}
          />
          {!researching && (
            <Button className="mt-3" size="sm" variant="ghost" onClick={replayResearch}>
              Искать заново
            </Button>
          )}
        </div>
      </Section>

      <Section title="Блоки: ai-answer">
        <div className="w-full max-w-2xl">
          <AiAnswer
            question="С чего начать, если api-gateway отдает 5xx?"
            thought="9s"
            reasoning={
              <>
                <p>Сверил алерт с runbook и прошлыми инцидентами.</p>
                <p>Чаще всего 5xx после выкладки – это сертификат или таймауты апстрима, их и проверяю первыми.</p>
              </>
            }
            sources={SOURCES}
            copyText="Проверь срок сертификата, потом логи за 15 минут."
            onRegenerate={() => undefined}
            feedback={feedback}
            onFeedback={setFeedback}
            shareLink="https://charter.example.com/answers/42"
            related={["Как продлить сертификат без простоя?", "Какие алерты стоит добавить на балансировщик?"]}
            onAsk={() => undefined}
          >
            <p>
              <Claim>
                Сначала проверь срок сертификата на балансировщике
                <Cite n={2} source={SOURCES[1]} />.
              </Claim>{" "}
              <Claim>
                Если он в порядке, открой логи сервиса за 15 минут и ищи всплеск таймаутов
                <Cite n={1} source={SOURCES[0]} />.
              </Claim>
            </p>
            <h4>Если не помогло</h4>
            <ol>
              <li>
                <strong>Сравни с дашбордом.</strong>{" "}
                <Claim>
                  Рост 5xx совпадает с выкладкой?
                  <Cite n={3} source={SOURCES[2]} />
                </Claim>
              </li>
              <li>
                <strong>Откати выкладку.</strong> Командой <code>argocd app rollback api-gateway</code> и напиши в канал инцидентов.
              </li>
            </ol>
            <AnswerTable>
              <thead>
                <tr>
                  <th />
                  <th>Сертификат</th>
                  <th>Таймауты</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>Где видно</td>
                  <td>Ошибки TLS в логах</td>
                  <td>504 и рост p99</td>
                </tr>
                <tr>
                  <td>Что делать</td>
                  <td>Продлить и перезагрузить</td>
                  <td>Откатить выкладку</td>
                </tr>
              </tbody>
            </AnswerTable>
            <AnswerCode
              file="check-cert.sh"
              lang="Shell"
              code={"echo | openssl s_client -connect api.example.com:443 2>/dev/null \\\n  | openssl x509 -noout -enddate"}
            />
          </AiAnswer>
        </div>
      </Section>

      <Section title="Блоки: ai-composer">
        <div className="w-full max-w-2xl">
          <AiComposer
            label="Сообщение"
            placeholder="Спроси что угодно…"
            value={text}
            onChange={setText}
            onSubmit={() => {
              setText("");
              setRunning(true);
            }}
            running={running}
            onStop={() => setRunning(false)}
            onAttach={() => undefined}
            tools={
              <>
                <ToolToggle on={web} onToggle={setWeb} icon={<Globe size={14} />}>
                  Поиск в сети
                </ToolToggle>
                <ToolToggle on={think} onToggle={setThink} icon={<Brain size={14} />}>
                  Подумать дольше
                </ToolToggle>
              </>
            }
            suggestions={[
              {
                id: "1",
                label: "Разбери последний алерт",
                icon: <Sparkles size={13} />,
              },
              {
                id: "2",
                label: "Что сломалось за ночь?",
                icon: <Search size={13} />,
              },
            ]}
          />
        </div>
      </Section>
    </>
  );
}

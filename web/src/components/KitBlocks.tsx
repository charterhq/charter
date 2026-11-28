import { Brain, FileText, Globe, GitBranch, MessageSquare, Search, Sparkles } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "./arc/button/button";
import { AgentPlan, type PlanStep } from "./blocks/AgentPlan";
import { AgentRun, type RunState, type RunStep } from "./blocks/AgentRun";
import { AiAnswer, Claim, Cite, type AnswerSource } from "./blocks/AiAnswer";
import { AiComposer, ToolToggle } from "./blocks/AiComposer";
import { ResearchRun, type ResearchQuery } from "./blocks/ResearchRun";
import { ToolCall, ToolCallGroup } from "./blocks/ToolCall";
import { Boat } from "./Mascot";

// Chat blocks on sample data for ?kit. The live UI shows the run and the brief writes; the rest waits
// for the core to report steps, tool calls, plans and sources.

const RUN: Omit<RunStep, "status">[] = [
  { id: "plan", icon: <Sparkles size={13} />, title: "Разобрал алерт и выбрал первый шаг", meta: "1.2s" },
  { id: "grafana", icon: <Search size={13} />, title: "Нашел панель в Grafana: api-gateway / 5xx", meta: "0.9s" },
  { id: "logs", icon: <FileText size={13} />, title: "Прочитал логи за 15 минут", meta: "2.4s" },
  { id: "post", icon: <MessageSquare size={13} />, title: "Написал дежурному в #alerts" },
];

// play walks a list of steps once on a button press; nothing runs on its own.
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
  { id: "s1", title: "Runbook: 5xx на api-gateway", site: "wiki", mark: "W" },
  { id: "s2", title: "Инцидент 14.10: истек сертификат", site: "jira", mark: "J" },
  { id: "s3", title: "Дашборд api-gateway", site: "grafana", mark: "G" },
];

const QUERIES: Omit<ResearchQuery, "status">[] = [
  { id: "q1", text: "runbook 5xx api-gateway", results: 4, mark: <FileText size={11} /> },
  { id: "q2", text: "инциденты api-gateway за месяц", results: 7, mark: <GitBranch size={11} /> },
  { id: "q3", text: "панели ошибок api-gateway", results: 2, mark: <Globe size={11} /> },
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
  const [planAt, replayPlan] = usePlay(plan.length, 900);
  const live: PlanStep[] = plan.map((s, i) => ({ ...s, status: i < planAt ? "done" : i === planAt ? "running" : "todo" }));

  const [qAt, replayResearch] = usePlay(QUERIES.length + 1, 1000);
  const researching = qAt <= QUERIES.length;

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
          <ToolCallGroup title="Работает с инструментами: 3" icons={[<Search key="s" size={11} />, <Globe key="g" size={11} />, <FileText key="f" size={11} />]}>
            <ToolCall icon={<Search size={13} />} title="Искал runbook по api-gateway" meta="4 результата · 1.2s" status="done" details={<pre className="whitespace-pre-wrap font-mono">{'{ "query": "runbook api-gateway 5xx" }'}</pre>} />
            <ToolCall icon={<Globe size={13} />} title="Открыл дашборд api-gateway" meta="760ms" status="done" />
            <ToolCall icon={<FileText size={13} />} title="Читает логи сервиса" status="running" />
            <ToolCall icon={<MessageSquare size={13} />} title="Написать в #alerts" status="waiting" details="Ждет подтверждения дежурного" />
            <ToolCall icon={<GitBranch size={13} />} title="Открыть MR с правкой" status="error" details="Нет прав на репозиторий" />
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
            actions={runState === "done" ? <Button size="sm" variant="secondary">Открыть прогон</Button> : undefined}
          />
          <AgentRun title="Ответ в беседе" agent="builder" state="failed" steps={[{ id: "x", title: "Модель не ответила за 60 с", status: "error" }]} onRetry={() => undefined} />
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
                setApproved(true);
                replayPlan();
              }}
            />
          ) : (
            <AgentPlan goal="Разбирать алерты Grafana и подсказывать дежурному" steps={live} />
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
            elapsed={researching ? undefined : "3s"}
            queries={QUERIES.slice(0, Math.min(qAt + 1, QUERIES.length)).map((q, i) => ({ ...q, status: i < qAt ? "done" : "running" }))}
            sources={SOURCES.map((s, i) => ({ ...s, read: i < qAt }))}
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
            reasoning="Сверил алерт с runbook и прошлыми инцидентами, выбрал самый частый случай."
            sources={SOURCES}
            copyText="Проверь срок сертификата, потом логи за 15 минут."
            onRegenerate={() => undefined}
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
            <h3 className="text-[16px] font-semibold">Если не помогло</h3>
            <ol className="list-decimal space-y-1 pl-5">
              <li>
                <Claim>
                  Сравни с дашбордом: рост 5xx совпадает с выкладкой?
                  <Cite n={3} source={SOURCES[2]} />
                </Claim>
              </li>
              <li>Откати выкладку и напиши в канал инцидентов.</li>
            </ol>
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
              { id: "1", label: "Разбери последний алерт", icon: <Sparkles size={13} /> },
              { id: "2", label: "Что сломалось за ночь?", icon: <Search size={13} /> },
            ]}
          />
        </div>
      </Section>
    </>
  );
}

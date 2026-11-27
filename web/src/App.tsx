import { useEffect, useState } from "react";
import { Canvas } from "./components/Canvas";
import { Chat } from "./components/Chat";
import { CommandPalette, type Action } from "./components/CommandPalette";
import { TopBar, type View } from "./components/TopBar";
import { Boat } from "./components/Mascot";
import { setToken } from "./lib/api";
import { boot, counted, draft, newAgent, useDraft } from "./lib/store";
import { cn } from "./lib/utils";

const THEME_KEY = "charter.theme";
const MOTION_KEY = "charter.reduce-motion";

function readMotion(): boolean {
  try {
    return localStorage.getItem(MOTION_KEY) === "1";
  } catch {
    return false;
  }
}

const PLACEHOLDER: Record<Exclude<View, "builder">, string> = {
  agents: "Каталог агентов появится, когда ядро отдаст его API",
  journal: "Журнал прогонов – следующий шаг",
  settings: "Настройки платформы появятся, когда ядро отдаст их API",
};

function readTheme(): boolean {
  try {
    return localStorage.getItem(THEME_KEY) !== "light";
  } catch {
    return true;
  }
}

export default function App() {
  const [view, setView] = useState<View>("builder");
  const [palette, setPalette] = useState(false);
  const [dark, setDark] = useState(readTheme);
  const [reduceMotion, setReduceMotion] = useState(readMotion);
  // Below lg the chat and the agent are two tabs; from lg up they sit side by side.
  const d = useDraft();
  const pane = d.pane;
  const setPane = (p: typeof pane) => draft.set({ pane: p });
  const total = counted(d).length;
  const filled = total - d.missing.length;

  useEffect(() => {
    void boot();
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.add("theme-switching");
    root.classList.toggle("dark", dark);
    // Transitions come back after the new colors are painted.
    const frame = requestAnimationFrame(() => requestAnimationFrame(() => root.classList.remove("theme-switching")));
    try {
      localStorage.setItem(THEME_KEY, dark ? "dark" : "light");
    } catch {
      // the theme still works for this visit
    }
    return () => cancelAnimationFrame(frame);
  }, [dark]);

  // "Меньше анимации" in the profile settings: the same rules as prefers-reduced-motion.
  useEffect(() => {
    document.documentElement.classList.toggle("reduce-motion", reduceMotion);
    try {
      localStorage.setItem(MOTION_KEY, reduceMotion ? "1" : "0");
    } catch {
      // the setting still works for this visit
    }
  }, [reduceMotion]);

  const actions: Action[] = [
    { id: "builder", label: "Создать агента", hint: "билдер", run: () => setView("builder") },
    {
      id: "new",
      label: "Новый черновик агента",
      run: () => {
        setView("builder");
        void newAgent();
      },
    },
    { id: "agents", label: "Все агенты", run: () => setView("agents") },
    { id: "journal", label: "Журнал прогонов", run: () => setView("journal") },
    { id: "settings", label: "Настройки платформы", run: () => setView("settings") },
    { id: "theme", label: dark ? "Светлая тема" : "Темная тема", run: () => setDark(!dark) },
  ];

  return (
    <>
      <div className="flex h-full flex-col">
        <TopBar
          view={view}
          onView={setView}
          onPalette={() => setPalette(true)}
          dark={dark}
          onDark={setDark}
          reduceMotion={reduceMotion}
          onReduceMotion={setReduceMotion}
        />
        {view === "builder" && d.status !== "ready" ? (
          <Gate />
        ) : view === "builder" ? (
          <main className="flex min-h-0 flex-1 flex-col gap-3 p-3 lg:grid lg:grid-cols-[minmax(300px,1fr)_minmax(0,2fr)] lg:gap-4 lg:p-4">
            <div role="tablist" aria-label="Билдер" className="grid shrink-0 grid-cols-2 gap-1 rounded-lg border bg-surface p-1 lg:hidden">
              {(
                [
                  ["chat", "Беседа"],
                  ["agent", `Агент · ${filled}/${total}`],
                ] as const
              ).map(([id, label]) => (
                <button
                  key={id}
                  role="tab"
                  aria-selected={pane === id}
                  onClick={() => setPane(id)}
                  className={cn(
                    "rounded-md py-1.5 text-[14px] font-medium tabular-nums text-muted-foreground",
                    pane === id && "bg-card text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
            <div className={cn("min-h-0 flex-1 flex-col lg:flex", pane === "chat" ? "flex" : "hidden")}>
              <Chat />
            </div>
            <div className={cn("min-h-0 flex-1 flex-col lg:flex", pane === "agent" ? "flex" : "hidden")}>
              <Canvas />
            </div>
          </main>
        ) : (
          <main className="grid flex-1 place-items-center p-4 text-center text-[15px] text-faint">
            {PLACEHOLDER[view]}
          </main>
        )}
      </div>
      <CommandPalette open={palette} onOpenChange={setPalette} actions={actions} />
      {d.notice && (
        <div role="status" className="fixed bottom-4 left-1/2 z-30 flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-3 rounded-lg border bg-card px-3 py-2 text-[14px] shadow-panel">
          <span className="min-w-0">{d.notice}</span>
          <button onClick={() => draft.set({ notice: undefined })} className="shrink-0 text-[13px] text-muted-foreground hover:text-foreground">
            Понятно
          </button>
        </div>
      )}
    </>
  );
}

// Until people sign in through OIDC, the UI asks once for an API token of the core.
function Gate() {
  const d = useDraft();
  const [value, setValue] = useState("");
  if (d.status === "loading")
    return (
      <main className="grid flex-1 place-items-center p-4 text-[15px] text-faint">
        <div className="flex flex-col items-center gap-3">
          <Boat size={40} bob />
          Загружаю черновик…
        </div>
      </main>
    );
  return (
    <main className="grid flex-1 place-items-center p-4">
      <form
        className="w-full max-w-sm rounded-xl border bg-card p-5 shadow-panel"
        onSubmit={(e) => {
          e.preventDefault();
          if (d.status === "token") setToken(value.trim());
          void boot();
        }}
      >
        <Boat size={36} />
        {d.status === "token" ? (
          <>
            <h1 className="mt-3 text-lg font-semibold">Вход</h1>
            <p className="mt-1 text-[14px] text-faint">
              Пока нет входа через Keycloak, нужен токен API ядра. Он хранится только в этом браузере.
            </p>
            <input
              autoFocus
              type="password"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              aria-label="Токен API"
              placeholder="Токен"
              className="mt-4 w-full rounded-lg border bg-surface px-3 py-2 text-[15px] outline-none focus:border-primary"
            />
          </>
        ) : (
          <>
            <h1 className="mt-3 text-lg font-semibold">Ядро не ответило</h1>
            <p className="mt-1 text-[14px] text-faint [overflow-wrap:anywhere]">{d.error}</p>
          </>
        )}
        {d.status === "token" && d.error && <p className="mt-2 text-[13px] text-destructive">{d.error}</p>}
        <button className="mt-4 w-full rounded-lg bg-primary px-4 py-2 text-[14px] font-semibold text-primary-foreground hover:bg-primary-hover">
          {d.status === "token" ? "Войти" : "Попробовать еще раз"}
        </button>
      </form>
    </main>
  );
}

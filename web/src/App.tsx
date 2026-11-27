import { useEffect, useState } from "react";
import { Canvas } from "./components/Canvas";
import { Chat } from "./components/Chat";
import { CommandPalette, type Action } from "./components/CommandPalette";
import { TopBar, type View } from "./components/TopBar";
import { FIELDS, draft, useDraft } from "./lib/store";
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
  const filled = FIELDS.filter((f) => d.brief[f]).length;

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
    { id: "new", label: "Создать агента", hint: "билдер", run: () => setView("builder") },
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
        {view === "builder" ? (
          <main className="flex min-h-0 flex-1 flex-col gap-3 p-3 lg:grid lg:grid-cols-[minmax(300px,1fr)_minmax(0,2fr)] lg:gap-4 lg:p-4">
            <div role="tablist" aria-label="Билдер" className="grid shrink-0 grid-cols-2 gap-1 rounded-lg border bg-surface p-1 lg:hidden">
              {(
                [
                  ["chat", "Беседа"],
                  ["agent", `Агент · ${filled}/${FIELDS.length}`],
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
    </>
  );
}

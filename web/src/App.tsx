import { AssistantRuntimeProvider, useLocalRuntime } from "@assistant-ui/react";
import { useEffect, useState } from "react";
import { Canvas } from "./components/Canvas";
import { Chat } from "./components/Chat";
import { CommandPalette, type Action } from "./components/CommandPalette";
import { TopBar, type View } from "./components/TopBar";
import { builderMock } from "./lib/builder-mock";

const THEME_KEY = "charter.theme";

function readTheme(): boolean {
  try {
    return localStorage.getItem(THEME_KEY) !== "light";
  } catch {
    return true;
  }
}

export default function App() {
  const runtime = useLocalRuntime(builderMock);
  const [view, setView] = useState<View>("canvas");
  const [palette, setPalette] = useState(false);
  const [dark, setDark] = useState(readTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    try {
      localStorage.setItem(THEME_KEY, dark ? "dark" : "light");
    } catch {
      // the theme still works for this visit
    }
  }, [dark]);

  const actions: Action[] = [
    { id: "new", label: "Новый агент", hint: "холст", run: () => setView("canvas") },
    { id: "agents", label: "Все агенты", run: () => setView("agents") },
    { id: "journal", label: "Журнал прогонов", run: () => setView("journal") },
    { id: "theme", label: dark ? "Светлая тема" : "Темная тема", run: () => setDark(!dark) },
  ];

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <div className="flex h-full flex-col">
        <TopBar view={view} onView={setView} onPalette={() => setPalette(true)} dark={dark} onTheme={() => setDark(!dark)} />
        {view === "canvas" ? (
          <main className="grid min-h-0 flex-1 gap-4 p-4 md:grid-cols-[minmax(300px,1fr)_2fr]">
            <Chat />
            <Canvas />
          </main>
        ) : (
          <main className="grid flex-1 place-items-center p-4 text-sm text-faint">
            {view === "agents" ? "Каталог агентов появится, когда ядро отдаст его API" : "Журнал прогонов – следующий шаг"}
          </main>
        )}
      </div>
      <CommandPalette open={palette} onOpenChange={setPalette} actions={actions} />
    </AssistantRuntimeProvider>
  );
}

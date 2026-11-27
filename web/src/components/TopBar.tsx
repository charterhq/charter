import { Moon, Sun } from "lucide-react";
import { Boat } from "./Mascot";
import { cn } from "@/lib/utils";

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export type View = "builder" | "agents" | "journal";

const NAV: { id: View; label: string }[] = [
  { id: "builder", label: "Создать агента" },
  { id: "agents", label: "Агенты" },
  { id: "journal", label: "Журнал" },
];

export function TopBar(props: {
  view: View;
  onView: (v: View) => void;
  onPalette: () => void;
  dark: boolean;
  onTheme: () => void;
}) {
  return (
    <header className="flex items-center gap-2 border-b px-3 py-2.5 sm:gap-4 sm:px-5 sm:py-3">
      <div className="flex shrink-0 items-center gap-2 text-[16px] font-bold tracking-tight">
        <Boat />
        <span className="hidden sm:inline">charter</span>
      </div>
      <nav className="flex min-w-0 gap-0.5 overflow-x-auto sm:ml-2">
        {NAV.map((n) => (
          <button
            key={n.id}
            onClick={() => props.onView(n.id)}
            aria-current={props.view === n.id ? "page" : undefined}
            className={cn(
              "shrink-0 whitespace-nowrap rounded-md px-2.5 py-1.5 text-[14px] sm:px-3 font-medium text-muted-foreground transition-colors hover:text-foreground",
              props.view === n.id && "bg-surface text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
            )}
          >
            {n.label}
          </button>
        ))}
      </nav>
      <button
        onClick={props.onPalette}
        className="ml-auto hidden min-w-56 items-center gap-2 rounded-md border px-3 py-1.5 text-[13px] text-faint hover:border-border-strong sm:flex"
      >
        Найти или сделать…
        <kbd className="ml-auto rounded border border-border-strong px-1.5 font-mono text-[12px] text-muted-foreground">{isMac ? "⌘K" : "Ctrl\u00a0K"}</kbd>
      </button>
      <button
        onClick={props.onTheme}
        aria-label="Сменить тему"
        className="ml-auto shrink-0 rounded-md border p-1.5 text-muted-foreground hover:text-foreground sm:ml-0"
      >
        {props.dark ? <Sun size={15} /> : <Moon size={15} />}
      </button>
    </header>
  );
}

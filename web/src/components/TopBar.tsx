import { Moon, Sun } from "lucide-react";
import { Boat } from "./Mascot";
import { cn } from "@/lib/utils";

export type View = "canvas" | "agents" | "journal";

const NAV: { id: View; label: string }[] = [
  { id: "canvas", label: "Холст" },
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
    <header className="flex items-center gap-4 border-b px-5 py-3">
      <div className="flex items-center gap-2 text-[15px] font-bold tracking-tight">
        <Boat />
        charter
      </div>
      <nav className="ml-2 flex gap-0.5">
        {NAV.map((n) => (
          <button
            key={n.id}
            onClick={() => props.onView(n.id)}
            className={cn(
              "rounded-md px-3 py-1.5 text-[13px] font-medium text-muted-foreground transition-colors hover:text-foreground",
              props.view === n.id && "bg-surface text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
            )}
          >
            {n.label}
          </button>
        ))}
      </nav>
      <button
        onClick={props.onPalette}
        className="ml-auto hidden min-w-56 items-center gap-2 rounded-md border px-3 py-1.5 text-xs text-faint hover:border-border-strong sm:flex"
      >
        Найти или сделать…
        <kbd className="ml-auto rounded border border-border-strong px-1.5 font-mono text-[11px] text-muted-foreground">⌘K</kbd>
      </button>
      <button
        onClick={props.onTheme}
        aria-label="Сменить тему"
        className="rounded-md border p-1.5 text-muted-foreground hover:text-foreground"
      >
        {props.dark ? <Sun size={15} /> : <Moon size={15} />}
      </button>
    </header>
  );
}

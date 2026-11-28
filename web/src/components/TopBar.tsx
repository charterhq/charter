import { Moon, Sun } from "lucide-react";
import { useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { Boat } from "./Mascot";
import { UserMenu, type Prefs } from "./UserMenu";
import { useT, type Messages } from "@/i18n";
import { cn } from "@/lib/utils";

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export type View = "builder" | "agents" | "journal" | "settings";

const NAV: View[] = ["builder", "agents", "journal", "settings"];
const navLabel = (t: Messages, v: View) => t.nav[v];

// Below sm the sections do not fit next to the logo and move into the avatar menu.
const narrowQuery = typeof window !== "undefined" ? window.matchMedia("(max-width: 639px)") : null;
const useNarrow = () =>
  useSyncExternalStore(
    (f) => {
      narrowQuery?.addEventListener("change", f);
      return () => narrowQuery?.removeEventListener("change", f);
    },
    () => narrowQuery?.matches ?? false,
  );

export function TopBar(
  props: Prefs & {
    view: View;
    // Signed out, the header keeps only the logo and the theme switch.
    signedIn: boolean;
    onView: (v: View) => void;
    onPalette: () => void;
  },
) {
  const t = useT();
  const narrow = useNarrow();
  // One pill slides under the active item instead of each item lighting up on its own.
  const nav = useRef<HTMLElement>(null);
  const [pill, setPill] = useState<{ x: number; w: number; animate: boolean } | null>(null);
  useLayoutEffect(() => {
    const el = nav.current?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!el) return;
    // The first placement and resizes jump; only a change of view slides.
    const place = (animate: boolean) => setPill({ x: el.offsetLeft, w: el.offsetWidth, animate });
    place(pill !== null);
    // The observer reports once on subscribe; skipping it keeps that slide alive.
    let subscribed = false;
    const ro = new ResizeObserver(() => (subscribed ? place(false) : (subscribed = true)));
    ro.observe(el);
    return () => ro.disconnect();
  }, [props.view, props.signedIn, t]);

  return (
    <header className="flex items-center gap-2 border-b px-3 py-2.5 sm:gap-4 sm:px-5 sm:py-3">
      <div className="flex shrink-0 items-center gap-2 font-display text-[17px] font-bold tracking-[-0.03em]">
        <Boat />
        <span className="hidden sm:inline">charter</span>
      </div>
      {/* On a narrow screen the header names the section; the sections are in the avatar menu. */}
      {props.signedIn && <span className="min-w-0 truncate text-[15px] font-semibold sm:hidden">{navLabel(t, props.view)}</span>}
      {props.signedIn && (
        <nav ref={nav} className="relative hidden min-w-0 gap-0.5 sm:ml-2 sm:flex">
          {pill && (
            <span
              aria-hidden
              className={cn(
                "absolute inset-y-0 left-0 rounded-md bg-surface shadow-[inset_0_0_0_1px_var(--border)]",
                pill.animate && "transition-[transform,width] duration-300 ease-[cubic-bezier(0.3,0.7,0.2,1)]",
              )}
              style={{ transform: `translateX(${pill.x}px)`, width: pill.w }}
            />
          )}
          {NAV.map((n) => (
            <button
              key={n}
              onClick={() => props.onView(n)}
              aria-current={props.view === n ? "page" : undefined}
              className={cn(
                "relative shrink-0 whitespace-nowrap rounded-md px-2.5 py-1.5 text-[14px] font-medium text-muted-foreground transition-colors duration-300 hover:text-foreground sm:px-3",
                props.view === n && "text-foreground",
              )}
            >
              {navLabel(t, n)}
            </button>
          ))}
        </nav>
      )}
      {props.signedIn && (
        <button
          onClick={props.onPalette}
          className="ml-auto hidden min-w-56 items-center gap-2 rounded-md border px-3 py-1.5 text-[13px] text-faint hover:border-border-strong sm:flex"
        >
          {t.nav.search}
          <kbd className="ml-auto rounded border border-border-strong px-1.5 font-mono text-[12px] text-muted-foreground">{isMac ? "⌘K" : "Ctrl\u00a0K"}</kbd>
        </button>
      )}
      {props.signedIn ? (
        <div className="ml-auto sm:ml-0">
          <UserMenu
            {...props}
            extra={
              narrow
                ? NAV.map((n) => ({ key: `nav-${n}`, label: navLabel(t, n), checked: props.view === n, onSelect: () => props.onView(n) }))
                : undefined
            }
          />
        </div>
      ) : (
        <span className="ml-auto" />
      )}
      <button
        onClick={() => props.onDark(!props.dark)}
        aria-label={t.nav.toggleTheme}
        className="shrink-0 rounded-md border p-1.5 text-muted-foreground hover:text-foreground"
      >
        {props.dark ? <Sun size={15} /> : <Moon size={15} />}
      </button>
    </header>
  );
}

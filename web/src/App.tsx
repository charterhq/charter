import { useEffect, useState } from "react";
import { Canvas } from "./components/Canvas";
import { Chat } from "./components/Chat";
import { CommandPalette, type Action } from "./components/CommandPalette";
import { TopBar, type View } from "./components/TopBar";
import { Boat } from "./components/Mascot";
import { useT } from "./i18n";
import { ApiError, authConfig, login, tokenLogin, type AuthConfig } from "./lib/api";
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

function readTheme(): boolean {
  try {
    return localStorage.getItem(THEME_KEY) !== "light";
  } catch {
    return true;
  }
}

export default function App() {
  const t = useT();
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

  // "Reduce motion" in the profile settings: the same rules as prefers-reduced-motion.
  useEffect(() => {
    document.documentElement.classList.toggle("reduce-motion", reduceMotion);
    try {
      localStorage.setItem(MOTION_KEY, reduceMotion ? "1" : "0");
    } catch {
      // the setting still works for this visit
    }
  }, [reduceMotion]);

  const actions: Action[] = [
    { id: "builder", label: t.palette.builder, hint: t.palette.builderHint, run: () => setView("builder") },
    {
      id: "new",
      label: t.palette.newDraft,
      run: () => {
        setView("builder");
        void newAgent();
      },
    },
    { id: "agents", label: t.palette.agents, run: () => setView("agents") },
    { id: "journal", label: t.palette.journal, run: () => setView("journal") },
    { id: "settings", label: t.palette.settings, run: () => setView("settings") },
    { id: "theme", label: dark ? t.palette.light : t.palette.dark, run: () => setDark(!dark) },
  ];

  return (
    <>
      <div className="flex h-full flex-col">
        <TopBar
          view={view}
          onView={setView}
          signedIn={d.status !== "login"}
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
            <div role="tablist" aria-label={t.app.tabs} className="grid shrink-0 grid-cols-2 gap-1 rounded-lg border bg-surface p-1 lg:hidden">
              {(
                [
                  ["chat", t.app.chatTab],
                  ["agent", t.app.agentTab(filled, total)],
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
            {t.placeholder[view]}
          </main>
        )}
      </div>
      <CommandPalette open={palette} onOpenChange={setPalette} actions={actions} />
      {d.notice && (
        <div role="status" className="fixed bottom-4 left-1/2 z-30 flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-3 rounded-lg border bg-popover px-3 py-2 text-[14px] shadow-floating">
          <span className="min-w-0">{d.notice}</span>
          <button onClick={() => draft.set({ notice: undefined })} className="shrink-0 text-[13px] text-muted-foreground hover:text-foreground">
            {t.app.gotIt}
          </button>
        </div>
      )}
    </>
  );
}

// Gate is the screen before the draft: loading, the login, or a core that did not answer.
function Gate() {
  const t = useT();
  const d = useDraft();
  if (d.status === "loading")
    return (
      <main className="grid flex-1 place-items-center p-4 text-[15px] text-faint">
        <div className="flex flex-col items-center gap-3">
          <Boat size={40} bob />
          {t.app.loading}
        </div>
      </main>
    );
  if (d.status === "login") return <Login error={d.error} />;
  return (
    <main className="grid flex-1 place-items-center p-4">
      <form
        className="w-full max-w-sm rounded-xl border bg-card p-5 shadow-panel"
        onSubmit={(e) => {
          e.preventDefault();
          void boot();
        }}
      >
        <Boat size={36} />
        <h1 className="mt-3 text-lg font-semibold">{t.app.coreDown}</h1>
        <p className="mt-1 text-[14px] text-faint [overflow-wrap:anywhere]">{d.error}</p>
        <button className="mt-4 w-full rounded-lg bg-primary px-4 py-2 text-[14px] font-semibold text-primary-foreground hover:bg-primary-hover">
          {t.app.retry}
        </button>
      </form>
    </main>
  );
}

// Login offers the provider when the core has one, and the platform's token while it has none (or as
// a way in for the platform when the provider is down). The token goes to the core once and is not kept.
function Login({ error }: { error?: string }) {
  const t = useT();
  const [cfg, setCfg] = useState<AuthConfig>();
  const [token, setToken] = useState("");
  const [tokenOpen, setTokenOpen] = useState(false);
  const [failed, setFailed] = useState<string>();
  useEffect(() => {
    authConfig()
      .then(setCfg)
      .catch(() => setCfg({ oidc: false, token: false }));
  }, []);
  const showToken = cfg && cfg.token && (!cfg.oidc || tokenOpen);
  return (
    <main className="grid flex-1 place-items-center p-4">
      <div className="w-full max-w-sm rounded-xl border bg-card p-5 shadow-panel">
        <Boat size={36} />
        <h1 className="mt-3 text-lg font-semibold">{t.login.title}</h1>
        {cfg && !cfg.oidc && !cfg.token && <p className="mt-1 text-[14px] text-faint">{t.login.off}</p>}
        {error && <p className="mt-2 text-[13px] text-destructive">{error}</p>}
        {cfg?.oidc && (
          <>
            <p className="mt-1 text-[14px] text-faint">{t.login.sso}</p>
            <button
              autoFocus
              onClick={login}
              className="mt-4 w-full rounded-lg bg-primary px-4 py-2 text-[14px] font-semibold text-primary-foreground hover:bg-primary-hover"
            >
              {t.login.signIn}
            </button>
          </>
        )}
        {cfg?.oidc && cfg.token && !tokenOpen && (
          <button onClick={() => setTokenOpen(true)} className="mt-3 w-full text-[13px] text-muted-foreground hover:text-foreground">
            {t.login.withToken}
          </button>
        )}
        {showToken && (
          <form
            className="mt-4"
            onSubmit={(e) => {
              e.preventDefault();
              setFailed(undefined);
              tokenLogin(token.trim())
                .then(() => {
                  setToken("");
                  return boot();
                })
                .catch((err) => setFailed(err instanceof ApiError && err.status === 403 ? t.login.notPlatform : t.login.badToken));
            }}
          >
            <p className="text-[14px] text-faint">{t.login.tokenHint}</p>
            <input
              autoFocus
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              aria-label={t.login.tokenLabel}
              placeholder={t.login.tokenPlaceholder}
              className="mt-3 w-full rounded-lg border bg-surface px-3 py-2 text-[15px] outline-none focus:border-primary"
            />
            {failed && <p className="mt-2 text-[13px] text-destructive">{failed}</p>}
            <button
              disabled={!token.trim()}
              className="mt-3 w-full rounded-lg border px-4 py-2 text-[14px] font-semibold hover:bg-surface disabled:opacity-50"
            >
              {t.login.tokenSubmit}
            </button>
          </form>
        )}
      </div>
    </main>
  );
}

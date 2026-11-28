import { LazyMotion, MotionConfig } from "motion/react";
import { lazy, Suspense, useEffect, useState } from "react";
import { Button } from "./components/arc/button/button";
import { Input } from "./components/arc/input/input";
import { ToastStack, ToastStackProvider, useToastStack } from "./components/arc/toast-stack/toast-stack";
import type { Action } from "./components/CommandPalette";
import { TopBar, type View } from "./components/TopBar";
import { Boat } from "./components/Mascot";
import { useT } from "./i18n";
import { ApiError, authConfig, login, tokenLogin, type AuthConfig } from "./lib/api";
import { boot, draft, newAgent, useDraft } from "./lib/store";

// The builder (the chat runtime and the canvas) and the palette load after the first screen.
const Builder = lazy(() => import("./components/Builder"));
const CommandPalette = lazy(() => import("./components/CommandPalette"));

const loadMotion = () => import("./lib/motion-features").then((r) => r.default);

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
  const d = useDraft();

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

  // ⌘K opens the palette from anywhere, before its code is loaded too.
  const [paletteUsed, setPaletteUsed] = useState(false);
  useEffect(() => {
    if (palette) setPaletteUsed(true);
  }, [palette]);
  useEffect(() => {
    if (d.status !== "ready") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setPalette((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [d.status]);

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
    // "Reduce motion" in the profile settings reaches the Arc components through MotionConfig.
    <MotionConfig reducedMotion={reduceMotion ? "always" : "user"}>
      <LazyMotion features={loadMotion}>
      <ToastStackProvider>
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
          <Suspense fallback={<Loading />}>
            <Builder />
          </Suspense>
        ) : (
          <main className="grid flex-1 place-items-center p-4 text-center text-[15px] text-faint">
            {t.placeholder[view]}
          </main>
        )}
      </div>
      {paletteUsed && (
        <Suspense>
          <CommandPalette open={palette} onOpenChange={setPalette} actions={actions} />
        </Suspense>
      )}
      <Notices />
      </ToastStackProvider>
      </LazyMotion>
    </MotionConfig>
  );
}

// A notice of the store (a conflict, a failed answer) becomes a toast and leaves the store at once.
function Notices() {
  const t = useT();
  const notice = useDraft().notice;
  const { toast } = useToastStack();
  useEffect(() => {
    if (!notice) return;
    toast({ type: "warning", title: notice });
    draft.set({ notice: undefined });
  }, [notice, toast]);
  return <ToastStack position="bottom-center" label={t.app.notices} strings={{ dismiss: t.app.dismiss, types: t.app.toastTypes }} />;
}

function Loading() {
  const t = useT();
  return (
    <main className="grid flex-1 place-items-center p-4 text-[15px] text-faint">
      <div className="flex flex-col items-center gap-3">
        <Boat size={40} bob />
        {t.app.loading}
      </div>
    </main>
  );
}

// Gate is the screen before the draft: loading, the login, or a core that did not answer.
function Gate() {
  const t = useT();
  const d = useDraft();
  if (d.status === "loading") return <Loading />;
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
        <Button type="submit" className="mt-4 w-full">
          {t.app.retry}
        </Button>
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
            <Button autoFocus onClick={login} className="mt-4 w-full">
              {t.login.signIn}
            </Button>
          </>
        )}
        {cfg?.oidc && cfg.token && !tokenOpen && (
          <Button variant="ghost" size="sm" onClick={() => setTokenOpen(true)} className="mt-2 w-full">
            {t.login.withToken}
          </Button>
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
            <Input
              autoFocus
              type="password"
              value={token}
              onChange={(e) => {
                setToken(e.target.value);
                setFailed(undefined);
              }}
              label={t.login.tokenLabel}
              description={t.login.tokenHint}
              error={failed}
              placeholder={t.login.tokenPlaceholder}
              autoComplete="off"
            />
            <Button type="submit" variant="secondary" disabled={!token.trim()} className="mt-3 w-full">
              {t.login.tokenSubmit}
            </Button>
          </form>
        )}
      </div>
    </main>
  );
}

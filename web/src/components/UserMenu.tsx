import { Command, Settings, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

// Until the core gives the signed-in user, the menu shows the draft's owner.
const USER = { login: "e.azimov", team: "SRE", role: "владелец агентов" };
const initials = (login: string) =>
  login
    .split(/[.\-_]/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0].toUpperCase())
    .join("");

export type Prefs = {
  dark: boolean;
  onDark: (dark: boolean) => void;
  reduceMotion: boolean;
  onReduceMotion: (on: boolean) => void;
};

export function UserMenu(props: Prefs & { onPalette: () => void }) {
  const [open, setOpen] = useState(false);
  const [settings, setSettings] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      button.current?.focus();
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  const item =
    "flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-[14px] text-muted-foreground hover:bg-surface hover:text-foreground focus-visible:bg-surface focus-visible:text-foreground focus-visible:outline-none";

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        ref={button}
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Пользователь ${USER.login}`}
        className="grid size-8 place-items-center rounded-full"
      >
        <Avatar />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-30 mt-2 w-72 animate-[pop-in_.14s_ease-out] rounded-xl border bg-card p-1.5 shadow-panel"
        >
          <div className="flex items-center gap-3 px-2.5 pb-2.5 pt-1.5">
            <Avatar size="lg" />
            <div className="min-w-0">
              <div className="truncate text-[14px] font-semibold">{USER.login}</div>
              <div className="truncate text-[13px] text-faint">
                команда {USER.team} · {USER.role}
              </div>
            </div>
          </div>
          <div className="mb-1 border-t" />
          <button
            role="menuitem"
            autoFocus
            className={item}
            onClick={() => {
              setOpen(false);
              setSettings(true);
            }}
          >
            <Settings size={15} aria-hidden />
            Настройки профиля
          </button>
          <button
            role="menuitem"
            className={item}
            onClick={() => {
              setOpen(false);
              props.onPalette();
            }}
          >
            <Command size={15} aria-hidden />
            Команды
            <kbd className="ml-auto font-mono text-[12px] text-faint">{isMac ? "⌘K" : "Ctrl K"}</kbd>
          </button>
        </div>
      )}
      <SettingsDialog open={settings} onClose={() => setSettings(false)} returnFocus={button} {...props} />
    </div>
  );
}

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

function Avatar({ size = "md" }: { size?: "md" | "lg" }) {
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center rounded-full bg-primary font-semibold text-primary-foreground shadow-[0_0_0_2px_var(--card),0_0_0_3px_var(--border-strong)]",
        size === "md" ? "size-7 text-[12px]" : "size-10 text-[14px]",
      )}
    >
      {initials(USER.login)}
    </span>
  );
}

// A native modal dialog: the browser dims the page, traps focus and closes it on Esc.
function SettingsDialog(
  props: Prefs & { open: boolean; onClose: () => void; returnFocus: React.RefObject<HTMLButtonElement | null> },
) {
  const ref = useRef<HTMLDialogElement>(null);
  const { open, onClose, returnFocus } = props;

  // Esc closes the dialog natively before `open` turns false, so focus goes back on every close.
  const wasOpen = useRef(false);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && wasOpen.current) {
      if (d.open) d.close();
      returnFocus.current?.focus();
    }
    wasOpen.current = open;
  }, [open, returnFocus]);

  return (
    <dialog
      ref={ref}
      aria-labelledby="settings-title"
      onClose={onClose}
      // A click on the dimmed backdrop lands on the dialog element itself.
      onClick={(e) => e.target === e.currentTarget && onClose()}
      className="m-auto w-[min(480px,calc(100vw-2rem))] rounded-2xl border bg-card p-0 text-foreground shadow-panel backdrop:bg-black/55 backdrop:backdrop-blur-[2px] open:animate-[pop-in_.18s_ease-out] open:backdrop:animate-[fade-in_.18s_ease-out]"
    >
      <div className="flex items-center gap-3 border-b px-5 py-4">
        <h2 id="settings-title" className="text-[16px] font-semibold tracking-tight">
          Настройки профиля
        </h2>
        <button
          onClick={onClose}
          aria-label="Закрыть"
          className="ml-auto grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-surface hover:text-foreground"
        >
          <X size={16} />
        </button>
      </div>

      <div className="space-y-6 px-5 py-5">
        <section>
          <h3 className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-faint">Профиль</h3>
          <div className="flex items-center gap-3">
            <Avatar size="lg" />
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-[14px]">
              <dt className="text-faint">Логин</dt>
              <dd className="font-mono text-[13px]">{USER.login}</dd>
              <dt className="text-faint">Команда</dt>
              <dd>{USER.team}</dd>
              <dt className="text-faint">Роль</dt>
              <dd>{USER.role}</dd>
            </dl>
          </div>
          <p className="mt-3 text-[13px] text-faint">Профиль приходит из учетной записи, здесь он только для чтения.</p>
        </section>

        <section>
          <h3 className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-faint">Внешний вид</h3>
          <Row label="Тема">
            <div role="radiogroup" aria-label="Тема" className="flex gap-1 rounded-lg border bg-surface p-1">
              {(
                [
                  [false, "Светлая"],
                  [true, "Темная"],
                ] as const
              ).map(([dark, label]) => (
                <button
                  key={label}
                  role="radio"
                  aria-checked={props.dark === dark}
                  onClick={() => props.onDark(dark)}
                  className={cn(
                    "rounded-md px-3 py-1 text-[13px] font-medium text-muted-foreground",
                    props.dark === dark && "bg-card text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
          </Row>
          <Row label="Меньше анимации" hint="Убирает движение: переезды, покачивание, подсветку полей">
            <Switch checked={props.reduceMotion} onChange={props.onReduceMotion} label="Меньше анимации" />
          </Row>
        </section>
      </div>
    </dialog>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-4 py-2">
      <div className="min-w-0 flex-1">
        <div className="text-[14px]">{label}</div>
        {hint && <div className="text-[13px] text-faint">{hint}</div>}
      </div>
      {children}
    </div>
  );
}

function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative h-6 w-10 shrink-0 rounded-full border transition-colors",
        checked ? "border-primary bg-primary" : "border-border-strong bg-surface",
      )}
    >
      <span
        className={cn(
          "absolute top-0.5 left-0.5 size-[18px] rounded-full shadow transition-transform",
          checked ? "translate-x-4 bg-primary-foreground" : "bg-muted-foreground",
        )}
      />
    </button>
  );
}

import { Command, LogOut, Settings } from "lucide-react";
import { lazy, Suspense, useRef, useState } from "react";
import { DropdownMenu, type DropdownItem } from "./arc/dropdown-menu/dropdown-menu";
import { useT } from "@/i18n";
import { logout } from "@/lib/api";
import { useDraft } from "@/lib/store";
import { cn } from "@/lib/utils";

const ProfileDialog = lazy(() => import("./ProfileDialog"));

// The signed-in person comes from GET /v1/me.
export function useUser() {
  const d = useDraft();
  return { login: d.me ?? "", name: d.meName ?? "" };
}
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

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

// `extra` are items shown first, such as the sections of the header on a narrow screen.
export function UserMenu(props: Prefs & { onPalette: () => void; extra?: DropdownItem[] }) {
  const t = useT();
  const user = useUser();
  // The dialog mounts on first open and stays, so it can animate out.
  const [settings, setSettings] = useState<boolean | undefined>();
  const button = useRef<HTMLButtonElement>(null);
  const extra = props.extra ?? [];

  const items: DropdownItem[] = [
    ...extra,
    {
      key: "settings",
      label: t.user.profileSettings,
      icon: <Settings size={15} />,
      separatorBefore: extra.length > 0,
      onSelect: () => setSettings(true),
    },
    {
      key: "commands",
      label: t.user.commands,
      icon: <Command size={15} />,
      hint: <kbd className="font-mono">{isMac ? "⌘K" : "Ctrl K"}</kbd>,
      onSelect: props.onPalette,
    },
    { key: "logout", label: t.user.signOut, icon: <LogOut size={15} />, separatorBefore: true, onSelect: () => void logout() },
  ];

  return (
    <>
      <DropdownMenu
        label={t.user.menu(user.login)}
        width="18rem"
        items={items}
        header={
          <div className="flex items-center gap-3">
            <Avatar size="lg" />
            <div className="min-w-0">
              <div className="truncate text-[14px] font-semibold">{user.name || user.login}</div>
              {user.name && <div className="truncate font-mono text-[13px] text-faint">{user.login}</div>}
            </div>
          </div>
        }
        trigger={
          <button ref={button} type="button" aria-label={t.user.menu(user.login)} className="grid size-8 shrink-0 place-items-center rounded-full">
            <Avatar />
          </button>
        }
      />
      {settings !== undefined && (
        <Suspense>
          <ProfileDialog open={settings} onClose={() => setSettings(false)} returnFocus={button} {...props} />
        </Suspense>
      )}
    </>
  );
}

export function Avatar({ size = "md" }: { size?: "md" | "lg" }) {
  const user = useUser();
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center rounded-full bg-primary font-semibold text-primary-foreground shadow-[0_0_0_2px_var(--card),0_0_0_3px_var(--border-strong)]",
        size === "md" ? "size-7 text-[12px]" : "size-10 text-[14px]",
      )}
    >
      {initials(user.login) || "?"}
    </span>
  );
}

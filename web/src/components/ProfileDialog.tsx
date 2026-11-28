import { Dialog, DialogContent } from "./arc/dialog/dialog";
import SegmentedControl from "./arc/segmented-control/segmented-control";
import { Switch } from "./arc/switch/switch";
import { Avatar, useUser, type Prefs } from "./UserMenu";
import { LANGS, setLang, useLang, useT, type Lang } from "@/i18n";

// The profile settings: who is signed in and how the interface looks. It loads on first open.
export default function ProfileDialog(
  props: Prefs & { open: boolean; onClose: () => void; returnFocus: React.RefObject<HTMLButtonElement | null> },
) {
  const t = useT();
  const lang = useLang();
  const user = useUser();
  return (
    <Dialog open={props.open} onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent
        title={t.user.profileSettings}
        closeLabel={t.user.close}
        // The dialog opens from a menu that is gone by then, so focus goes back to the avatar.
        onCloseAutoFocus={(e) => {
          e.preventDefault();
          props.returnFocus.current?.focus();
        }}
        className="w-[min(480px,calc(100vw-2rem))]"
      >
        <div className="space-y-6">
          <section>
            <h3 className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-faint">{t.user.profile}</h3>
            <div className="flex items-center gap-3">
              <Avatar size="lg" />
              <dl className="grid min-w-0 grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-[14px]">
                <dt className="text-faint">{t.user.login}</dt>
                <dd className="truncate font-mono text-[13px]">{user.login}</dd>
                <dt className="text-faint">{t.user.name}</dt>
                <dd className="truncate">{user.name || "–"}</dd>
              </dl>
            </div>
            <p className="mt-3 text-[13px] text-faint">{t.user.profileHint}</p>
          </section>

          <section>
            <h3 className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-faint">{t.user.appearance}</h3>
            <Row label={t.user.language}>
              <SegmentedControl
                label={t.user.language}
                value={lang}
                onValueChange={(l) => setLang(l as Lang)}
                options={Object.entries(LANGS).map(([id, l]) => ({ value: id, label: l.name }))}
              />
            </Row>
            <Row label={t.user.theme}>
              <SegmentedControl
                label={t.user.theme}
                value={props.dark ? "dark" : "light"}
                onValueChange={(v) => props.onDark(v === "dark")}
                options={[
                  { value: "light", label: t.user.light },
                  { value: "dark", label: t.user.dark },
                ]}
              />
            </Row>
            <Row label={t.user.reduceMotion} hint={t.user.reduceMotionHint}>
              <Switch aria-label={t.user.reduceMotion} checked={props.reduceMotion} onCheckedChange={props.onReduceMotion} />
            </Row>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-4 py-2">
      <div className="min-w-0 flex-1">
        <div className="text-[14px]">{label}</div>
        {hint && <div className="text-[13px] text-faint">{hint}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

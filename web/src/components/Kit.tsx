import { LazyMotion, domMax } from "motion/react";
import { Bell, Bold, Italic, Pencil, Trash2, Underline } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Button } from "./arc/button/button";
import { Checkbox } from "./arc/checkbox/checkbox";
import { Dialog, DialogClose, DialogContent, DialogTrigger } from "./arc/dialog/dialog";
import { DropdownMenu } from "./arc/dropdown-menu/dropdown-menu";
import { Input } from "./arc/input/input";
import { Popover, PopoverContent, PopoverTrigger } from "./arc/popover/popover";
import SegmentedControl from "./arc/segmented-control/segmented-control";
import { Select } from "./arc/select/select";
import { Switch } from "./arc/switch/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./arc/tabs/tabs";
import { Textarea } from "./arc/textarea/textarea";
import { ToastStack, ToastStackProvider, useToastStack } from "./arc/toast-stack/toast-stack";
import { ToggleGroup, ToggleGroupItem } from "./arc/toggle-group/toggle-group";
import { Tooltip } from "./arc/tooltip/tooltip";
import { KitBlocks } from "./KitBlocks";

// A development page with every base component in our theme: open the UI with ?kit.
// It is loaded lazily and only in development, so it never reaches the build.

export default function Kit() {
  return (
    <LazyMotion features={domMax}>
      <ToastStackProvider>
        <KitBody />
        <ToastStack />
      </ToastStackProvider>
    </LazyMotion>
  );
}

function KitBody() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  const [mode, setMode] = useState("auto");
  const [model, setModel] = useState("reasoning");
  const [notify, setNotify] = useState(true);
  const [agree, setAgree] = useState(false);
  const { toast } = useToastStack();

  const toggleTheme = () => {
    document.documentElement.classList.toggle("dark");
    setDark(!dark);
  };

  return (
    <main className="mx-auto max-w-5xl space-y-6 p-8">
      <header className="flex items-center justify-between">
        <h1 className="text-[22px] font-semibold">Базовые компоненты</h1>
        <Button variant="secondary" size="sm" onClick={toggleTheme}>
          {dark ? "Светлая тема" : "Темная тема"}
        </Button>
      </header>

      <Section title="Кнопки">
        <Button>Подтвердить заявку</Button>
        <Button variant="secondary">Черновик</Button>
        <Button variant="ghost">Отмена</Button>
        <Button variant="danger">Остановить агента</Button>
        <Button loading>Сохраняю</Button>
        <Button size="sm" variant="secondary">
          <Pencil size={14} /> Править
        </Button>
      </Section>

      <Section title="Переключатели">
        <Switch label="Уведомлять в Mattermost" checked={notify} onCheckedChange={setNotify} />
        <Checkbox label="Агент может писать в Jira" description="Только комментарии" checked={agree} onCheckedChange={(v) => setAgree(v === true)} />
        <SegmentedControl
          label="Режим"
          value={mode}
          onValueChange={setMode}
          options={[
            { value: "auto", label: "Сам" },
            { value: "ask", label: "Спрашивать" },
            { value: "shadow", label: "Тень" },
          ]}
        />
        <ToggleGroup label="Стиль текста" type="multiple">
          <ToggleGroupItem value="b" label="Жирный">
            <Bold size={16} />
          </ToggleGroupItem>
          <ToggleGroupItem value="i" label="Курсив">
            <Italic size={16} />
          </ToggleGroupItem>
          <ToggleGroupItem value="u" label="Подчеркнутый">
            <Underline size={16} />
          </ToggleGroupItem>
        </ToggleGroup>
      </Section>

      <Section title="Меню и выбор">
        <DropdownMenu
          label="Действия"
          items={[
            { label: "Переименовать", icon: <Pencil size={14} /> },
            { label: "Подписаться", icon: <Bell size={14} /> },
            { label: "Удалить черновик", icon: <Trash2 size={14} />, destructive: true, separatorBefore: true },
          ]}
        />
        <div className="w-64">
          <Select
            label="Модель"
            value={model}
            onValueChange={setModel}
            options={[
              { value: "reasoning", label: "reasoning-light" },
              { value: "fast", label: "fast" },
              { value: "large", label: "large", disabled: true },
            ]}
          />
        </div>
        <Popover>
          <PopoverTrigger asChild>
            <Button variant="secondary">Поповер</Button>
          </PopoverTrigger>
          <PopoverContent>
            <p className="max-w-56 text-[14px]">Значение пришло из беседы "Первый разговор", 11:10.</p>
          </PopoverContent>
        </Popover>
        <Tooltip content="Откроет бриф">
          <Button variant="ghost">Наведи</Button>
        </Tooltip>
      </Section>

      <Section title="Поля">
        <div className="grid w-full items-start gap-4 sm:grid-cols-2">
          <Input label="Название агента" placeholder="Разбор алертов" />
          <Input label="Канал" defaultValue="#alerts" error="Канал не найден" />
          <div className="sm:col-span-2">
            <Textarea label="Зачем агент" description="Своими словами, пара предложений" />
          </div>
        </div>
      </Section>

      <Section title="Вкладки">
        <Tabs defaultValue="brief" className="w-full">
          <TabsList>
            <TabsTrigger value="brief">Бриф</TabsTrigger>
            <TabsTrigger value="package">Пакет</TabsTrigger>
            <TabsTrigger value="eval">Проверка</TabsTrigger>
          </TabsList>
          <TabsContent value="brief">
            <p className="pt-3 text-[14px] text-muted-foreground">Что агент должен делать.</p>
          </TabsContent>
          <TabsContent value="package">
            <p className="pt-3 text-[14px] text-muted-foreground">agent.yml и промпты.</p>
          </TabsContent>
          <TabsContent value="eval">
            <p className="pt-3 text-[14px] text-muted-foreground">Набор проверки.</p>
          </TabsContent>
        </Tabs>
      </Section>

      <Section title="Диалог и тосты">
        <Dialog>
          <DialogTrigger asChild>
            <Button variant="secondary">Открыть диалог</Button>
          </DialogTrigger>
          <DialogContent title="Подтвердить заявку?" description="Билдер заведет эпик и перейдет к описанию агента.">
            <div className="flex justify-end gap-2 pt-4">
              <DialogClose asChild>
                <Button variant="ghost">Отмена</Button>
              </DialogClose>
              <DialogClose asChild>
                <Button>Подтвердить</Button>
              </DialogClose>
            </div>
          </DialogContent>
        </Dialog>
        <Button variant="secondary" onClick={() => toast({ type: "success", title: "Заявка подтверждена", description: "Эпик заведен" })}>
          Тост: успех
        </Button>
        <Button variant="secondary" onClick={() => toast({ type: "error", title: "Не удалось сохранить", description: "Поле изменили в другой беседе" })}>
          Тост: ошибка
        </Button>
      </Section>

      <KitBlocks Section={Section} />
    </main>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border bg-card p-5 shadow-panel">
      <h2 className="mb-4 text-[13px] font-medium text-muted-foreground">{title}</h2>
      <div className="flex flex-wrap items-center gap-3">{children}</div>
    </section>
  );
}

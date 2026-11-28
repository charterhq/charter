// The core's public API: the UI talks to nothing else. People sign in through the core's OIDC
// login; the session is an HttpOnly cookie the UI never sees.

// The builder is an ordinary agent with memory: object; the installation may name it differently.
export const BUILDER = {
  agent: (import.meta.env.VITE_BUILDER_AGENT as string | undefined) ?? "builder",
  connection: (import.meta.env.VITE_BUILDER_CONNECTION as string | undefined) ?? "platform",
};

// The API sits next to the UI: /charter/ui/ → /charter/v1/, /ui/ → /v1/.
const BASE = new URL("../v1/", new URL(".", location.href)).pathname.replace(/\/$/, "");

// The UI before the provider login kept an API token in localStorage: drop it.
try {
  localStorage.removeItem("charter.token");
} catch {
  // storage is off: nothing was kept either
}

// login sends the browser to the provider; it comes back to the page it left.
export function login() {
  location.href = `${BASE}/auth/login?return_to=${encodeURIComponent(location.pathname + location.search)}`;
}

export type AuthConfig = { oidc: boolean; token: boolean };

// authConfig says which logins the core offers.
export async function authConfig(): Promise<AuthConfig> {
  return api<AuthConfig>("GET", "/auth/config");
}

// tokenLogin trades a platform token for a session cookie; the token is not kept anywhere.
export async function tokenLogin(token: string): Promise<void> {
  const res = await fetch(`${BASE}/auth/token`, {
    method: "POST",
    headers: { "X-Charter-Request": "1", Authorization: `Bearer ${token}` },
  });
  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) throw new ApiError(res.status, String(data.code ?? "internal"), String(data.error ?? res.statusText), data);
}

// logout ends the session in the core, then at the provider.
export async function logout() {
  const out = await api<{ logout_url?: string }>("POST", "/auth/logout", { return_to: location.pathname }).catch(() => ({}) as { logout_url?: string });
  if (out.logout_url) location.href = out.logout_url;
  else location.reload();
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public body: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

function headers(json: boolean): HeadersInit {
  // with the session cookie the core takes changes only with this header (CSRF)
  const h: Record<string, string> = { "X-Charter-Request": "1" };
  if (json) h["Content-Type"] = "application/json";
  return h;
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    headers: headers(body !== undefined),
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) throw new ApiError(res.status, String(data.code ?? "internal"), String(data.error ?? res.statusText), data);
  return data as T;
}

// Shapes of the core's answers (internal/run/object.go).

export type Field = {
  name: string;
  title?: string;
  question?: string;
  type?: string;
  required?: boolean;
  collection?: boolean;
  min_items?: number;
  readonly_model?: boolean;
};

export type FieldSource = {
  rev: number;
  source: "conversation" | "person";
  conversation_id?: string;
  run_id?: string;
  by: string;
  at: string;
};

export type Stage = { id: string; title: string; artifacts?: { kind: string; title: string }[] };
export type Member = { person: string; role: "owner" | "member" };
export type Conversation = { id: string; title: string; opened_by: string; created_at: string };

export type Message = {
  seq: number;
  message_id?: string;
  conversation: string;
  role: "user" | "agent";
  author: string;
  text: string;
  run_id?: string;
  fields: string[];
  at: string;
};

export type ObjectView = {
  id: string;
  agent: string;
  connection: string;
  title: string;
  attrs: Record<string, unknown>;
  state: Record<string, unknown>;
  rev: number;
  fields: Field[];
  missing: string[];
  sources: Record<string, FieldSource>;
  stage: string;
  stages: Stage[];
  members: Member[];
  conversations: Conversation[];
  last_seq: number;
  created_by: string;
  archived_at?: string;
};

export type ObjectSummary = { id: string; title: string; stage: string; missing: number; updated_at: string };

export type ObjectEvent = { seq: number; type: string; data: any };

// Server-sent events over fetch, to send the CSRF header and read statuses. The stream resumes after the
// last event it saw; "reset" means events were lost and the snapshot must be read again.
export function subscribe(
  objectID: string,
  after: number,
  on: { event: (e: ObjectEvent) => void; reset: () => void; status?: (live: boolean) => void },
): () => void {
  const ctl = new AbortController();
  let last = after;
  let delay = 500;
  const run = async () => {
    while (!ctl.signal.aborted) {
      try {
        const res = await fetch(`${BASE}/objects/${objectID}/events?after=${last}`, {
          headers: { ...headers(false), Accept: "text/event-stream" },
          signal: ctl.signal,
        });
        if (!res.ok || !res.body) throw new Error(`events: ${res.status}`);
        on.status?.(true);
        delay = 500;
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let i: number;
          while ((i = buf.indexOf("\n\n")) >= 0) {
            const block = buf.slice(0, i);
            buf = buf.slice(i + 2);
            let id = "";
            let type = "";
            let data = "";
            for (const line of block.split("\n")) {
              if (line.startsWith("id: ")) id = line.slice(4);
              else if (line.startsWith("event: ")) type = line.slice(7);
              else if (line.startsWith("data: ")) data += line.slice(6);
            }
            if (type === "reset") {
              on.reset();
              return;
            }
            if (!type) continue;
            if (id) last = Number(id);
            on.event({ seq: last, type, data: data ? JSON.parse(data) : null });
          }
        }
      } catch {
        if (ctl.signal.aborted) return;
      }
      on.status?.(false);
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, 10_000);
    }
  };
  void run();
  return () => ctl.abort();
}

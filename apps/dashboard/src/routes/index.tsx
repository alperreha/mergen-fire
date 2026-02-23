import { createMemo, createResource, For, Match, Show, Switch } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { Button } from "~/components/ui/button";

type Dict = Record<string, unknown>;

type VMListResponse = {
  items: VMSummary[];
};

type VMSummary = {
  id: string;
  createdAt: string;
  metadata?: Dict;
  systemd: {
    active: boolean;
    activeState?: string;
    subState?: string;
    unit?: string;
  };
  network: {
    guestIP: string;
    ports: PortBinding[];
    tapName: string;
    netns: string;
  };
};

type PortBinding = {
  guest: number;
  host: number;
  protocol: string;
};

type VMMetadata = {
  id: string;
  createdAt: string;
  guestIP: string;
  httpPort?: number;
  metadata?: Dict;
  tags?: Record<string, string>;
};

type VMConfig = Dict;
type VMHooks = Dict;

type VMDetails = {
  meta: VMMetadata;
  vmConfig: VMConfig;
  hooks: VMHooks;
};

type FlatRow = {
  key: string;
  value: string;
};

const API_BASE = (import.meta.env.VITE_MERGEN_API_BASE as string | undefined)?.trim() || "";

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`);
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`HTTP ${response.status} ${response.statusText}${body ? `: ${body}` : ""}`);
  }
  return (await response.json()) as T;
}

async function fetchVMs(): Promise<VMListResponse> {
  return fetchJSON<VMListResponse>("/v1/vms");
}

async function fetchMetaMap(ids: string[] | undefined): Promise<Record<string, VMMetadata | null>> {
  if (!ids || ids.length === 0) {
    return {};
  }
  const results = await Promise.all(
    ids.map(async id => {
      try {
        const meta = await fetchJSON<VMMetadata>(`/v1/vms/${id}/meta.json`);
        return [id, meta] as const;
      } catch {
        return [id, null] as const;
      }
    })
  );
  return Object.fromEntries(results);
}

async function fetchVMDetails(id: string | null): Promise<VMDetails | null> {
  if (!id) {
    return null;
  }
  const [meta, vmConfig, hooks] = await Promise.all([
    fetchJSON<VMMetadata>(`/v1/vms/${id}/meta.json`),
    fetchJSON<VMConfig>(`/v1/vms/${id}/vm.json`),
    fetchJSON<VMHooks>(`/v1/vms/${id}/hooks.json`),
  ]);
  return { meta, vmConfig, hooks };
}

function valueToText(value: unknown): string {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function flattenObject(value: unknown): FlatRow[] {
  const rows: FlatRow[] = [];

  const walk = (node: unknown, keyPath: string) => {
    if (Array.isArray(node)) {
      if (node.length === 0) {
        rows.push({ key: keyPath || "(root)", value: "[]" });
        return;
      }
      node.forEach((entry, index) => {
        const next = keyPath ? `${keyPath}[${index}]` : `[${index}]`;
        walk(entry, next);
      });
      return;
    }

    if (node !== null && typeof node === "object") {
      const entries = Object.entries(node as Dict);
      if (entries.length === 0) {
        rows.push({ key: keyPath || "(root)", value: "{}" });
        return;
      }
      entries.forEach(([key, entry]) => {
        const next = keyPath ? `${keyPath}.${key}` : key;
        walk(entry, next);
      });
      return;
    }

    rows.push({ key: keyPath || "(root)", value: valueToText(node) });
  };

  walk(value, "");
  return rows;
}

function preferredName(meta: VMMetadata | null | undefined, vm: VMSummary): string {
  const tags = meta?.tags ?? {};
  const metadata = meta?.metadata ?? {};
  const candidates = [
    tags.name,
    tags.app,
    tags.host,
    tags.hostname,
    typeof metadata.name === "string" ? metadata.name : "",
    typeof metadata.app === "string" ? metadata.app : "",
    typeof metadata.host === "string" ? metadata.host : "",
    typeof metadata.hostname === "string" ? metadata.hostname : "",
  ].filter(Boolean);

  if (candidates.length > 0) {
    return candidates[0] as string;
  }
  return vm.id.slice(0, 8);
}

function subdomainLabels(meta: VMMetadata | null | undefined): string[] {
  const tags = meta?.tags ?? {};
  const metadata = meta?.metadata ?? {};
  const values = [
    tags.host,
    tags.hostname,
    tags.app,
    tags.name,
    typeof metadata.host === "string" ? metadata.host : "",
    typeof metadata.hostname === "string" ? metadata.hostname : "",
    typeof metadata.app === "string" ? metadata.app : "",
    typeof metadata.name === "string" ? metadata.name : "",
  ].filter(Boolean) as string[];
  return [...new Set(values)];
}

function tagEntries(meta: VMMetadata | null | undefined): Array<[string, string]> {
  return Object.entries(meta?.tags ?? {});
}

function formatTimestamp(raw: string | undefined): string {
  if (!raw) return "-";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return date.toLocaleString();
}

function JsonSection(props: { title: string; payload: unknown }) {
  const rows = createMemo(() => flattenObject(props.payload));

  return (
    <section class="rounded-xl border border-border/70 bg-card/95 shadow-sm">
      <header class="flex items-center justify-between border-b border-border/70 px-4 py-3">
        <h3 class="font-semibold tracking-tight text-card-foreground">{props.title}</h3>
        <span class="rounded-full border border-border px-2 py-0.5 text-xs text-muted-foreground">
          {rows().length} satır
        </span>
      </header>
      <div class="max-h-[28rem] overflow-auto">
        <Show
          when={rows().length > 0}
          fallback={<p class="px-4 py-5 text-sm text-muted-foreground">Bu JSON içinde gösterilecek veri bulunamadı.</p>}
        >
          <table class="w-full border-collapse text-left text-sm">
            <thead class="sticky top-0 z-10 bg-muted/75 backdrop-blur">
              <tr>
                <th class="w-[42%] border-b border-border px-4 py-2 font-medium text-muted-foreground">Key</th>
                <th class="border-b border-border px-4 py-2 font-medium text-muted-foreground">Value</th>
              </tr>
            </thead>
            <tbody>
              <For each={rows()}>
                {row => (
                  <tr class="align-top odd:bg-background even:bg-muted/20">
                    <td class="border-b border-border px-4 py-2 font-mono text-xs text-foreground">{row.key}</td>
                    <td class="border-b border-border px-4 py-2 break-all font-mono text-xs text-muted-foreground">
                      {row.value}
                    </td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </Show>
      </div>
    </section>
  );
}

export default function Home() {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = createMemo(() => {
    const value = searchParams.vm;
    return typeof value === "string" && value.trim() ? value.trim() : null;
  });

  const [vmList, { refetch: refetchVMList }] = createResource(fetchVMs);

  const vmIDs = createMemo(() => vmList()?.items.map(vm => vm.id));
  const [metaMap, { refetch: refetchMetaMap }] = createResource(vmIDs, fetchMetaMap);

  const selectedSummary = createMemo(() =>
    vmList()?.items.find(vm => vm.id === selectedId()) ?? null
  );

  const [vmDetails, { refetch: refetchVMDetails }] = createResource(selectedId, fetchVMDetails);

  const refreshAll = async () => {
    await refetchVMList();
    await refetchMetaMap();
    if (selectedId()) {
      await refetchVMDetails();
    }
  };

  return (
    <div class="min-h-screen bg-[radial-gradient(circle_at_top,_oklch(0.98_0.02_210)_0%,_oklch(0.995_0.002_260)_45%,_oklch(1_0_0)_100%)]">
      <header class="border-b border-border/70 bg-white/80 backdrop-blur-sm">
        <div class="container py-8">
          <h1 class="text-center text-3xl font-semibold tracking-[0.12em] text-slate-800 sm:text-4xl">
            MERGEN
          </h1>
          <p class="mt-2 text-center text-sm text-muted-foreground">
            Centralized VM Dashboard
          </p>
        </div>
      </header>

      <main class="container py-8">
        <div class="mb-6 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-xl font-semibold tracking-tight text-slate-800">
              <Show when={selectedId()} fallback="VM Listesi">
                VM Detayı
              </Show>
            </h2>
            <p class="text-sm text-muted-foreground">
              <Show
                when={selectedId()}
                fallback={`Toplam ${vmList()?.items.length ?? 0} VM bulundu.`}
              >
                VM dosyaları: meta.json, vm.json, hooks.json
              </Show>
            </p>
          </div>
          <div class="flex items-center gap-2">
            <Show when={selectedId()}>
              <Button
                variant="outline"
                onClick={() => setSearchParams({})}
                class="border-slate-300 text-slate-700"
              >
                Ana sayfaya geri dön
              </Button>
            </Show>
            <Button onClick={refreshAll} class="bg-slate-800 text-slate-100 hover:bg-slate-700">
              Yenile
            </Button>
          </div>
        </div>

        <Show when={selectedId()}>
          <nav class="mb-5 rounded-lg border border-border/70 bg-white/70 px-3 py-2 text-sm">
            <button
              type="button"
              class="font-medium text-slate-700 hover:underline"
              onClick={() => setSearchParams({})}
            >
              Ana sayfa
            </button>
            <span class="mx-2 text-muted-foreground">/</span>
            <span class="font-mono text-xs text-muted-foreground">{selectedId()}</span>
          </nav>
        </Show>

        <Switch>
          <Match when={!selectedId()}>
            <Switch>
              <Match when={vmList.loading}>
                <div class="grid gap-4 md:grid-cols-3">
                  <For each={[0, 1, 2, 3, 4, 5]}>
                    {index => (
                      <div class="h-44 animate-pulse rounded-xl border border-border/70 bg-card/70 p-4">
                        <div class="h-4 w-2/3 rounded bg-muted" />
                        <div class="mt-3 h-3 w-1/2 rounded bg-muted" />
                        <div class="mt-10 h-3 w-full rounded bg-muted" />
                        <div class="mt-2 h-3 w-5/6 rounded bg-muted" />
                        <span class="sr-only">loading-{index}</span>
                      </div>
                    )}
                  </For>
                </div>
              </Match>
              <Match when={vmList.error}>
                <div class="rounded-xl border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
                  VM listesi alınamadı: {String(vmList.error)}
                </div>
              </Match>
              <Match when={(vmList()?.items.length ?? 0) === 0}>
                <div class="rounded-xl border border-border/70 bg-card p-6 text-sm text-muted-foreground">
                  Bu makinede listelenecek VM bulunamadı.
                </div>
              </Match>
              <Match when={true}>
                <div class="grid gap-4 md:grid-cols-3">
                  <For each={vmList()?.items ?? []}>
                    {vm => {
                      const meta = createMemo(() => metaMap()?.[vm.id] ?? null);
                      const labels = createMemo(() => subdomainLabels(meta()));
                      const tags = createMemo(() => tagEntries(meta()));
                      return (
                        <button
                          type="button"
                          class="group rounded-xl border border-border/70 bg-card/95 p-4 text-left shadow-sm transition hover:-translate-y-0.5 hover:border-slate-300 hover:shadow-md"
                          onClick={() => setSearchParams({ vm: vm.id })}
                        >
                          <div class="mb-3 flex items-center justify-between gap-2">
                            <h3 class="truncate text-lg font-semibold tracking-tight text-card-foreground">
                              {preferredName(meta(), vm)}
                            </h3>
                            <span
                              classList={{
                                "rounded-full border px-2 py-0.5 text-xs font-medium": true,
                                "border-emerald-200 bg-emerald-50 text-emerald-700": vm.systemd.active,
                                "border-zinc-200 bg-zinc-50 text-zinc-600": !vm.systemd.active,
                              }}
                            >
                              {vm.systemd.active ? "Active" : "Inactive"}
                            </span>
                          </div>

                          <p class="mb-1 font-mono text-xs text-muted-foreground">{vm.id}</p>
                          <p class="mb-3 text-xs text-muted-foreground">
                            Oluşturulma: {formatTimestamp(vm.createdAt)}
                          </p>

                          <div class="space-y-1 text-sm text-slate-700">
                            <p>
                              <span class="text-muted-foreground">Guest IP:</span> {vm.network.guestIP || "-"}
                            </p>
                            <p>
                              <span class="text-muted-foreground">Subdomain:</span>{" "}
                              {labels().length > 0 ? labels().slice(0, 3).join(", ") : "-"}
                            </p>
                          </div>

                          <div class="mt-3 flex flex-wrap gap-1.5">
                            <Show
                              when={tags().length > 0}
                              fallback={
                                <span class="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-xs text-zinc-600">
                                  tags: yok
                                </span>
                              }
                            >
                              <For each={tags().slice(0, 4)}>
                                {([key, value]) => (
                                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 font-mono text-xs text-slate-700">
                                    {key}={value}
                                  </span>
                                )}
                              </For>
                            </Show>
                          </div>
                        </button>
                      );
                    }}
                  </For>
                </div>
              </Match>
            </Switch>
          </Match>

          <Match when={selectedId()}>
            <Switch>
              <Match when={vmDetails.loading}>
                <div class="rounded-xl border border-border/70 bg-card p-5 text-sm text-muted-foreground">
                  VM detayları yükleniyor...
                </div>
              </Match>
              <Match when={vmDetails.error}>
                <div class="rounded-xl border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
                  VM detayları alınamadı: {String(vmDetails.error)}
                </div>
              </Match>
              <Match when={!selectedSummary()}>
                <div class="rounded-xl border border-border/70 bg-card p-5 text-sm text-muted-foreground">
                  Seçilen VM liste içinde bulunamadı.
                </div>
              </Match>
              <Match when={vmDetails()}>
                <div class="space-y-4">
                  <div class="rounded-xl border border-border/70 bg-card/90 p-4">
                    <h3 class="font-semibold text-card-foreground">{selectedSummary()?.id}</h3>
                    <p class="mt-1 text-sm text-muted-foreground">
                      Guest IP: {selectedSummary()?.network.guestIP || "-"} • Unit:{" "}
                      {selectedSummary()?.systemd.unit || "-"}
                    </p>
                  </div>

                  <div class="grid gap-4 lg:grid-cols-3">
                    <JsonSection title="meta.json" payload={vmDetails()?.meta ?? {}} />
                    <JsonSection title="vm.json" payload={vmDetails()?.vmConfig ?? {}} />
                    <JsonSection title="hooks.json" payload={vmDetails()?.hooks ?? {}} />
                  </div>
                </div>
              </Match>
            </Switch>
          </Match>
        </Switch>
      </main>

      <footer class="border-t border-border/70 bg-white/70">
        <div class="container py-4 text-center text-xs text-muted-foreground">
          Mergen Dashboard • VM lifecycle and artifacts visibility
        </div>
      </footer>
    </div>
  );
}

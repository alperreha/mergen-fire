import { A, useSearchParams } from "@solidjs/router";
import { createEffect, createMemo, createResource, createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore } from "solid-js/store";

import { DashboardShell } from "~/components/dashboard-shell";
import { Button } from "~/components/ui/button";
import type {
  ConvertedImage,
  ConverterConvertResponse,
  ConverterImagesResponse,
} from "~/lib/converter";
import { formatTimestamp, toPortSummary } from "~/lib/json";
import { createVM, listVMs, type CreateVMRequest } from "~/lib/mergen-api";
import { cn } from "~/lib/utils";

type TabKey = "vm-list" | "vm-create";

type CreateForm = {
  rootfs: string;
  agentDisk: string;
  payloadDisk: string;
  envDisk: string;
  kernel: string;
  vcpu: string;
  memMiB: string;
  httpPort: string;
  guestPort: string;
  hostPort: string;
  appTag: string;
  metadataImage: string;
  autoStart: boolean;
};

const DEFAULT_FORM: CreateForm = {
  rootfs: "",
  agentDisk: "",
  payloadDisk: "",
  envDisk: "",
  kernel: "/var/lib/mergen/base/vmlinux",
  vcpu: "1",
  memMiB: "512",
  httpPort: "80",
  guestPort: "80",
  hostPort: "0",
  appTag: "",
  metadataImage: "",
  autoStart: true,
};

async function parseError(response: Response): Promise<string> {
  const raw = await response.text();
  if (!raw) {
    return `HTTP ${response.status} ${response.statusText}`;
  }
  try {
    const parsed = JSON.parse(raw) as { message?: string; details?: { stderr?: string } };
    const message = parsed.message?.trim();
    if (message) {
      const stderr = parsed.details?.stderr?.trim();
      return stderr ? `${message}: ${stderr}` : message;
    }
  } catch {
    // no-op
  }
  return `HTTP ${response.status}: ${raw}`;
}

async function fetchConvertedImages(): Promise<ConverterImagesResponse> {
  const response = await fetch("/api/converter/images");
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return (await response.json()) as ConverterImagesResponse;
}

async function runConvertImage(image: string): Promise<ConverterConvertResponse> {
  const response = await fetch("/api/converter/convert", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ image }),
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return (await response.json()) as ConverterConvertResponse;
}

function getTab(raw: string | undefined): TabKey {
  if (raw === "vm-create") {
    return "vm-create";
  }
  return "vm-list";
}

function normalizePort(raw: string, fallback: number): number {
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed)) {
    return fallback;
  }
  return parsed;
}

function imageTagFromName(raw: string): string {
  const normalized = raw.trim();
  if (!normalized) {
    return "";
  }
  const parts = normalized.split("/");
  const tail = parts[parts.length - 1] || normalized;
  return tail.replace(/[^a-zA-Z0-9._-]/g, "-");
}

export default function Home() {
  const [searchParams] = useSearchParams<{ tab?: string }>();
  const activeTab = createMemo<TabKey>(() => getTab(searchParams.tab));

  const [vmList, { refetch: refetchVMList }] = createResource(listVMs);
  const [showInactive, setShowInactive] = createSignal(false);

  const visibleVMs = createMemo(() => {
    const items = vmList()?.items ?? [];
    if (showInactive()) {
      return items;
    }
    return items.filter(vm => vm.systemd.active);
  });

  const [converterData, { refetch: refetchConverterData, mutate: mutateConverterData }] =
    createResource(fetchConvertedImages);
  const [selectedImageID, setSelectedImageID] = createSignal<string | null>(null);

  const [converterImageName, setConverterImageName] = createSignal("");
  const [convertPending, setConvertPending] = createSignal(false);
  const [convertError, setConvertError] = createSignal<string | null>(null);
  const [convertNotice, setConvertNotice] = createSignal<string | null>(null);

  const [createForm, setCreateForm] = createStore<CreateForm>({ ...DEFAULT_FORM });
  const [createPending, setCreatePending] = createSignal(false);
  const [createError, setCreateError] = createSignal<string | null>(null);
  const [createdVMID, setCreatedVMID] = createSignal<string | null>(null);

  const selectedImage = createMemo<ConvertedImage | null>(() => {
    const id = selectedImageID();
    if (!id) {
      return null;
    }
    return converterData()?.items.find(item => item.id === id) ?? null;
  });

  createEffect(() => {
    const items = converterData()?.items ?? [];
    if (items.length === 0) {
      return;
    }

    const selected = selectedImageID();
    if (selected && items.some(item => item.id === selected)) {
      return;
    }

    const defaultImage = items.find(item => item.ready) ?? items[0];
    setSelectedImageID(defaultImage.id);
  });

  const applyImageToVMForm = (image: ConvertedImage) => {
    const suggested = image.suggestedRequest ?? {};
    const suggestedPorts = Array.isArray(suggested.ports) && suggested.ports.length > 0
      ? suggested.ports[0]
      : null;
    const suggestedMetadata =
      suggested.metadata && typeof suggested.metadata === "object"
        ? (suggested.metadata as Record<string, unknown>)
        : {};
    const imageName = image.image || String(suggestedMetadata.image || "");

    setCreateForm({
      rootfs: typeof suggested.rootfs === "string" ? suggested.rootfs : image.paths.rootfs,
      agentDisk: typeof suggested.agentDisk === "string" ? suggested.agentDisk : image.paths.agentDisk,
      payloadDisk:
        typeof suggested.payloadDisk === "string" ? suggested.payloadDisk : image.paths.payloadDisk,
      envDisk: typeof suggested.envDisk === "string" ? suggested.envDisk : image.paths.envDisk,
      kernel: typeof suggested.kernel === "string" ? suggested.kernel : createForm.kernel,
      vcpu: typeof suggested.vcpu === "number" ? String(suggested.vcpu) : createForm.vcpu,
      memMiB: typeof suggested.memMiB === "number" ? String(suggested.memMiB) : createForm.memMiB,
      httpPort:
        typeof suggested.httpPort === "number"
          ? String(suggested.httpPort)
          : suggestedPorts && typeof suggestedPorts.guest === "number"
            ? String(suggestedPorts.guest)
            : createForm.httpPort,
      guestPort:
        suggestedPorts && typeof suggestedPorts.guest === "number"
          ? String(suggestedPorts.guest)
          : createForm.guestPort,
      hostPort:
        suggestedPorts && typeof suggestedPorts.host === "number"
          ? String(suggestedPorts.host)
          : createForm.hostPort,
      metadataImage: imageName,
      appTag: createForm.appTag || imageTagFromName(imageName),
    });
    setSelectedImageID(image.id);
  };

  const handleConvertSubmit = async (event: SubmitEvent) => {
    event.preventDefault();
    setConvertNotice(null);
    setConvertError(null);

    const image = converterImageName().trim();
    if (!image) {
      setConvertError("Docker/OCI image adı zorunlu. Örn: nginx:alpine");
      return;
    }

    setConvertPending(true);
    try {
      const result = await runConvertImage(image);
      mutateConverterData(() => result.images);
      setConvertNotice(`Image başarıyla dönüştürüldü: ${result.conversion.outputDir}`);
      setConverterImageName("");

      const matched = result.images.items.find(item => item.outputDir === result.conversion.outputDir);
      if (matched) {
        applyImageToVMForm(matched);
      }
    } catch (error) {
      setConvertError(error instanceof Error ? error.message : "Image dönüştürme başarısız oldu");
    } finally {
      setConvertPending(false);
    }
  };

  const handleCreateSubmit = async (event: SubmitEvent) => {
    event.preventDefault();
    setCreateError(null);
    setCreatedVMID(null);

    const vcpu = normalizePort(createForm.vcpu, 1);
    const memMiB = normalizePort(createForm.memMiB, 512);
    const httpPort = normalizePort(createForm.httpPort, 80);
    const guestPort = normalizePort(createForm.guestPort, 80);
    const hostPort = normalizePort(createForm.hostPort, 0);

    const request: CreateVMRequest = {
      rootfs: createForm.rootfs.trim(),
      agentDisk: createForm.agentDisk.trim(),
      payloadDisk: createForm.payloadDisk.trim(),
      envDisk: createForm.envDisk.trim(),
      kernel: createForm.kernel.trim(),
      vcpu,
      memMiB,
      httpPort,
      ports: [{ guest: guestPort, host: hostPort, protocol: "tcp" }],
      autoStart: createForm.autoStart,
    };

    if (!request.rootfs || !request.agentDisk || !request.payloadDisk || !request.envDisk || !request.kernel) {
      setCreateError("rootfs, agentDisk, payloadDisk, envDisk ve kernel alanları zorunludur.");
      return;
    }

    if (createForm.metadataImage.trim()) {
      request.metadata = { image: createForm.metadataImage.trim() };
    }
    if (createForm.appTag.trim()) {
      request.tags = { app: createForm.appTag.trim() };
    }

    setCreatePending(true);
    try {
      const result = await createVM(request);
      setCreatedVMID(result.id);
      await refetchVMList();
    } catch (error) {
      setCreateError(error instanceof Error ? error.message : "VM oluşturulamadı");
    } finally {
      setCreatePending(false);
    }
  };

  return (
    <DashboardShell
      activeTab={activeTab()}
      title={activeTab() === "vm-list" ? "VM Listele" : "VM Oluştur + Converter"}
      subtitle={
        activeTab() === "vm-list"
          ? "Systemd tarafından yönetilen MicroVM durumlarını canlı olarak izleyin."
          : "Container image dönüştürüp aynı ekrandan VM create akışını tamamlayın."
      }
    >
      <Switch>
        <Match when={activeTab() === "vm-list"}>
          <section class="space-y-5">
            <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-white/85 p-4 shadow-sm">
              <div>
                <p class="text-sm text-slate-700">
                  Görünen VM sayısı: <strong>{visibleVMs().length}</strong>
                </p>
                <p class="text-xs text-muted-foreground">
                  Toplam kayıt: {vmList()?.items.length ?? 0}
                </p>
              </div>
              <div class="flex items-center gap-2">
                <label class="flex items-center gap-2 text-sm text-slate-700">
                  <input
                    type="checkbox"
                    checked={showInactive()}
                    onInput={event => setShowInactive(event.currentTarget.checked)}
                  />
                  Duran VM'leri de göster
                </label>
                <Button variant="outline" onClick={refetchVMList}>
                  Yenile
                </Button>
              </div>
            </div>

            <Switch>
              <Match when={vmList.loading}>
                <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
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

              <Match when={visibleVMs().length === 0}>
                <div class="rounded-xl border border-border/70 bg-card p-6 text-sm text-muted-foreground">
                  Gösterilecek VM bulunamadı.
                </div>
              </Match>

              <Match when={true}>
                <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  <For each={visibleVMs()}>
                    {vm => (
                      <A
                        href={`/vms/${vm.id}`}
                        class="group rounded-xl border border-border/70 bg-card/95 p-4 text-left shadow-sm transition-all duration-200 hover:-translate-y-0.5 hover:border-slate-300 hover:shadow-md"
                      >
                        <div class="mb-3 flex items-center justify-between gap-2">
                          <h3 class="truncate text-lg font-semibold tracking-tight text-card-foreground">
                            {vm.id.slice(0, 12)}
                          </h3>
                          <span
                            classList={{
                              "rounded-full border px-2 py-0.5 text-xs font-medium transition-colors": true,
                              "border-emerald-200 bg-emerald-50 text-emerald-700": vm.systemd.active,
                              "border-zinc-200 bg-zinc-50 text-zinc-600": !vm.systemd.active,
                            }}
                          >
                            {vm.systemd.active ? "ACTIVE" : "INACTIVE"}
                          </span>
                        </div>

                        <p class="mb-1 font-mono text-xs text-muted-foreground">{vm.id}</p>
                        <p class="mb-2 text-xs text-muted-foreground">
                          Oluşturulma: {formatTimestamp(vm.createdAt)}
                        </p>

                        <div class="space-y-1 text-sm text-slate-700">
                          <p>
                            <span class="text-muted-foreground">Guest IP:</span> {vm.network.guestIP || "-"}
                          </p>
                          <p>
                            <span class="text-muted-foreground">Portlar:</span> {toPortSummary(vm.network.ports)}
                          </p>
                          <p>
                            <span class="text-muted-foreground">Unit:</span> {vm.systemd.unit || "-"}
                          </p>
                        </div>

                        <p class="mt-3 text-xs font-medium text-slate-600 transition group-hover:text-slate-900">
                          Detaya git
                        </p>
                      </A>
                    )}
                  </For>
                </div>
              </Match>
            </Switch>
          </section>
        </Match>

        <Match when={activeTab() === "vm-create"}>
          <div class="grid gap-6 xl:grid-cols-[1.2fr_1fr]">
            <section class="space-y-4 rounded-xl border border-slate-200 bg-white/90 p-4 shadow-sm">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h3 class="text-base font-semibold text-slate-800">Converter Image Havuzu</h3>
                  <p class="text-xs text-muted-foreground">
                    Base path: {converterData()?.baseDir || "/var/lib/mergen/images"}
                  </p>
                </div>
                <div class="flex items-center gap-2">
                  <span class="rounded-full border border-slate-200 bg-slate-100 px-2 py-1 text-xs text-slate-700">
                    image: {converterData()?.total ?? 0}
                  </span>
                  <Button variant="outline" onClick={refetchConverterData}>
                    Yenile
                  </Button>
                </div>
              </div>

              <form class="rounded-lg border border-slate-200 bg-slate-50/60 p-3" onSubmit={handleConvertSubmit}>
                <label class="mb-1 block text-sm font-medium text-slate-700">Yeni image ekle</label>
                <div class="flex flex-wrap items-center gap-2">
                  <input
                    type="text"
                    value={converterImageName()}
                    onInput={event => setConverterImageName(event.currentTarget.value)}
                    class="min-w-[18rem] flex-1 rounded-md border border-slate-300 bg-white px-3 py-2 text-sm outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    placeholder="örn: nginx:alpine"
                  />
                  <Button type="submit" disabled={convertPending()}>
                    {convertPending() ? "Dönüştürülüyor..." : "Image Ekle ve Dönüştür"}
                  </Button>
                </div>
                <Show when={convertNotice()}>
                  <p class="mt-2 text-xs text-emerald-700">{convertNotice()}</p>
                </Show>
                <Show when={convertError()}>
                  <p class="mt-2 text-xs text-destructive">{convertError()}</p>
                </Show>
                <Show when={converterData()?.activeConversion}>
                  <p class="mt-2 text-xs text-slate-600">
                    Aktif dönüşüm: {converterData()?.activeConversion?.image} (
                    {formatTimestamp(converterData()?.activeConversion?.startedAt)})
                  </p>
                </Show>
              </form>

              <Switch>
                <Match when={converterData.loading}>
                  <p class="text-sm text-muted-foreground">Converter image listesi yükleniyor...</p>
                </Match>
                <Match when={converterData.error}>
                  <p class="text-sm text-destructive">Image listesi alınamadı: {String(converterData.error)}</p>
                </Match>
                <Match when={(converterData()?.items.length ?? 0) === 0}>
                  <p class="rounded-lg border border-slate-200 bg-slate-50 p-4 text-sm text-muted-foreground">
                    Henüz dönüştürülmüş image bulunamadı.
                  </p>
                </Match>
                <Match when={true}>
                  <div class="max-h-[28rem] space-y-2 overflow-auto pr-1">
                    <For each={converterData()?.items ?? []}>
                      {image => (
                        <button
                          type="button"
                          onClick={() => applyImageToVMForm(image)}
                          class={cn(
                            "w-full rounded-lg border p-3 text-left transition-all duration-200 hover:border-slate-300 hover:bg-slate-50",
                            selectedImageID() === image.id
                              ? "border-slate-800 bg-slate-50 shadow-[0_0_0_1px_rgba(15,23,42,0.3)]"
                              : "border-slate-200 bg-white"
                          )}
                        >
                          <div class="mb-2 flex items-center justify-between gap-2">
                            <p class="truncate text-sm font-semibold text-slate-800">{image.image}</p>
                            <span
                              class={cn(
                                "rounded-full border px-2 py-0.5 text-[11px] font-semibold",
                                image.ready
                                  ? "border-emerald-200 bg-emerald-50 text-emerald-700"
                                  : "border-amber-200 bg-amber-50 text-amber-700"
                              )}
                            >
                              {image.ready ? "READY" : "INCOMPLETE"}
                            </span>
                          </div>
                          <p class="truncate font-mono text-[11px] text-muted-foreground">{image.outputDir}</p>
                          <p class="mt-1 text-[11px] text-muted-foreground">
                            Güncellendi: {formatTimestamp(image.updatedAt)}
                          </p>
                          <p class="mt-2 text-[11px] text-slate-600">
                            rootfs:{image.artifacts.rootfs ? "yes" : "no"} • agent:
                            {image.artifacts.agentDisk ? "yes" : "no"} • payload:
                            {image.artifacts.payloadDisk ? "yes" : "no"} • env:
                            {image.artifacts.envDisk ? "yes" : "no"}
                          </p>
                        </button>
                      )}
                    </For>
                  </div>
                </Match>
              </Switch>
            </section>

            <section
              class={cn(
                "rounded-xl border border-slate-200 bg-white/90 p-4 shadow-sm transition-shadow duration-300",
                createPending() ? "animate-soft-pulse" : ""
              )}
            >
              <h3 class="text-base font-semibold text-slate-800">VM Create Form</h3>
              <p class="mb-4 text-xs text-muted-foreground">
                Seçili image: <span class="font-medium">{selectedImage()?.image ?? "-"}</span>
              </p>

              <form class="space-y-3" onSubmit={handleCreateSubmit}>
                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">rootfs</span>
                  <input
                    value={createForm.rootfs}
                    onInput={event => setCreateForm("rootfs", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">agentDisk</span>
                  <input
                    value={createForm.agentDisk}
                    onInput={event => setCreateForm("agentDisk", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">payloadDisk</span>
                  <input
                    value={createForm.payloadDisk}
                    onInput={event => setCreateForm("payloadDisk", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">envDisk</span>
                  <input
                    value={createForm.envDisk}
                    onInput={event => setCreateForm("envDisk", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">kernel</span>
                  <input
                    value={createForm.kernel}
                    onInput={event => setCreateForm("kernel", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <div class="grid gap-3 sm:grid-cols-2">
                  <label class="block">
                    <span class="mb-1 block text-sm text-slate-700">vcpu</span>
                    <input
                      value={createForm.vcpu}
                      onInput={event => setCreateForm("vcpu", event.currentTarget.value)}
                      class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    />
                  </label>
                  <label class="block">
                    <span class="mb-1 block text-sm text-slate-700">memMiB</span>
                    <input
                      value={createForm.memMiB}
                      onInput={event => setCreateForm("memMiB", event.currentTarget.value)}
                      class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    />
                  </label>
                </div>

                <div class="grid gap-3 sm:grid-cols-3">
                  <label class="block">
                    <span class="mb-1 block text-sm text-slate-700">httpPort</span>
                    <input
                      value={createForm.httpPort}
                      onInput={event => setCreateForm("httpPort", event.currentTarget.value)}
                      class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    />
                  </label>
                  <label class="block">
                    <span class="mb-1 block text-sm text-slate-700">guestPort</span>
                    <input
                      value={createForm.guestPort}
                      onInput={event => setCreateForm("guestPort", event.currentTarget.value)}
                      class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    />
                  </label>
                  <label class="block">
                    <span class="mb-1 block text-sm text-slate-700">hostPort</span>
                    <input
                      value={createForm.hostPort}
                      onInput={event => setCreateForm("hostPort", event.currentTarget.value)}
                      class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                    />
                  </label>
                </div>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">tag (app)</span>
                  <input
                    value={createForm.appTag}
                    onInput={event => setCreateForm("appTag", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="block">
                  <span class="mb-1 block text-sm text-slate-700">metadata.image</span>
                  <input
                    value={createForm.metadataImage}
                    onInput={event => setCreateForm("metadataImage", event.currentTarget.value)}
                    class="w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
                  />
                </label>

                <label class="flex items-center gap-2 text-sm text-slate-700">
                  <input
                    type="checkbox"
                    checked={createForm.autoStart}
                    onInput={event => setCreateForm("autoStart", event.currentTarget.checked)}
                  />
                  Oluşturulduğunda otomatik başlat
                </label>

                <div class="flex flex-wrap items-center gap-2">
                  <Button type="submit" disabled={createPending()}>
                    {createPending() ? "Oluşturuluyor..." : "VM Oluştur"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setCreateForm({ ...DEFAULT_FORM })}
                    disabled={createPending()}
                  >
                    Formu Sıfırla
                  </Button>
                </div>

                <Show when={createError()}>
                  <p class="text-sm text-destructive">{createError()}</p>
                </Show>
                <Show when={createdVMID()}>
                  <p class="text-sm text-emerald-700">
                    VM oluşturuldu: <span class="font-mono">{createdVMID()}</span>{" "}
                    <A href={`/vms/${createdVMID()}`} class="underline">
                      Detaya git
                    </A>
                  </p>
                </Show>
              </form>
            </section>
          </div>
        </Match>
      </Switch>
    </DashboardShell>
  );
}

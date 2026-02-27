import { A, useNavigate, useParams } from "@solidjs/router";
import { createMemo, createResource, createSignal, Match, Show, Switch } from "solid-js";

import { DashboardShell } from "~/components/dashboard-shell";
import { JsonSection } from "~/components/json-section";
import { Button } from "~/components/ui/button";
import { formatTimestamp, toPortSummary } from "~/lib/json";
import { deleteVM, getVM, getVMConfig, getVMHooks, getVMMeta, startVM, stopVM } from "~/lib/mergen-api";
import { cn } from "~/lib/utils";

type VMDetailBundle = {
  vm: Awaited<ReturnType<typeof getVM>>;
  meta: Awaited<ReturnType<typeof getVMMeta>> | null;
  vmConfig: Awaited<ReturnType<typeof getVMConfig>> | null;
  hooks: Awaited<ReturnType<typeof getVMHooks>> | null;
};

type LifecycleAction = "stop" | "start" | "delete";

async function loadVMDetail(id: string): Promise<VMDetailBundle> {
  const vm = await getVM(id);
  const [meta, vmConfig, hooks] = await Promise.all([
    getVMMeta(id).catch(() => null),
    getVMConfig(id).catch(() => null),
    getVMHooks(id).catch(() => null),
  ]);

  return {
    vm,
    meta,
    vmConfig,
    hooks,
  };
}

export default function VMDetailsRoute() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const vmID = createMemo(() => (params.id || "").trim());

  const [vmBundle, { refetch }] = createResource(vmID, loadVMDetail);

  const [actionInFlight, setActionInFlight] = createSignal<LifecycleAction | null>(null);
  const [actionError, setActionError] = createSignal<string | null>(null);
  const [actionNotice, setActionNotice] = createSignal<string | null>(null);

  const currentVM = createMemo(() => vmBundle()?.vm ?? null);
  const isActive = createMemo(() => currentVM()?.systemd.active ?? false);
  const isBusy = createMemo(() => actionInFlight() !== null);

  const runLifecycleAction = async (
    action: LifecycleAction,
    operation: () => Promise<unknown>,
    successMessage: string
  ) => {
    setActionError(null);
    setActionNotice(null);
    setActionInFlight(action);
    try {
      await operation();
      setActionNotice(successMessage);
      if (action === "delete") {
        await navigate("/?tab=vm-list");
        return;
      }
      await refetch();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "VM lifecycle işlemi başarısız oldu");
    } finally {
      setActionInFlight(null);
    }
  };

  return (
    <DashboardShell
      activeTab="vm-list"
      title="VM Detay"
      subtitle="Stop / silme / yeniden başlatma işlemlerini bu ekrandan yönetin."
    >
      <section class="space-y-5">
        <A href="/?tab=vm-list" class="inline-flex text-sm font-medium text-slate-700 hover:underline">
          ← VM listesine dön
        </A>

        <Switch>
          <Match when={vmBundle.loading}>
            <div class="rounded-xl border border-border/70 bg-card p-5 text-sm text-muted-foreground">
              VM detayları yükleniyor...
            </div>
          </Match>

          <Match when={vmBundle.error}>
            <div class="rounded-xl border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
              VM detayları alınamadı: {String(vmBundle.error)}
            </div>
          </Match>

          <Match when={vmBundle()}>
            <div class="space-y-4 animate-lift-in">
              <article
                class={cn(
                  "rounded-xl border bg-white/90 p-4 shadow-sm transition-all duration-300",
                  isBusy() ? "border-slate-300 animate-soft-pulse" : "border-slate-200"
                )}
              >
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <h3 class="font-mono text-sm text-slate-600">{currentVM()?.id}</h3>
                    <p class="mt-1 text-sm text-slate-700">
                      Oluşturulma: {formatTimestamp(currentVM()?.createdAt)}
                    </p>
                    <p class="mt-1 text-sm text-slate-700">
                      Guest IP: {currentVM()?.network.guestIP || "-"} • Portlar:{" "}
                      {toPortSummary(currentVM()?.network.ports ?? [])}
                    </p>
                    <p class="mt-1 text-sm text-slate-700">
                      Unit: {currentVM()?.systemd.unit || "-"} ({currentVM()?.systemd.activeState || "-"} /{" "}
                      {currentVM()?.systemd.subState || "-"})
                    </p>
                  </div>

                  <span
                    classList={{
                      "rounded-full border px-3 py-1 text-xs font-semibold transition-colors": true,
                      "border-emerald-200 bg-emerald-50 text-emerald-700": isActive(),
                      "border-zinc-200 bg-zinc-50 text-zinc-600": !isActive(),
                    }}
                  >
                    {isActive() ? "ACTIVE" : "INACTIVE"}
                  </span>
                </div>

                <div class="mt-4 flex flex-wrap gap-2">
                  <Button
                    onClick={() =>
                      runLifecycleAction("stop", () => stopVM(vmID()), "VM stop komutu gönderildi ve durum güncellendi.")
                    }
                    disabled={!isActive() || isBusy()}
                    class="bg-amber-600 text-white hover:bg-amber-500"
                  >
                    {actionInFlight() === "stop" ? "Stopping..." : "Stop"}
                  </Button>

                  <Button
                    variant="outline"
                    onClick={() =>
                      runLifecycleAction(
                        "start",
                        () => startVM(vmID()),
                        "VM yeniden başlatıldı ve systemd aktif duruma geçti."
                      )
                    }
                    disabled={isActive() || isBusy()}
                  >
                    {actionInFlight() === "start" ? "Starting..." : "Yeniden Başlat"}
                  </Button>

                  <Button
                    variant="destructive"
                    onClick={() =>
                      runLifecycleAction("delete", () => deleteVM(vmID()), "VM silindi, listeye yönlendiriliyorsunuz.")
                    }
                    disabled={isActive() || isBusy()}
                  >
                    {actionInFlight() === "delete" ? "Deleting..." : "Sil"}
                  </Button>

                  <Button variant="outline" onClick={refetch} disabled={isBusy()}>
                    Yenile
                  </Button>
                </div>

                <Show when={actionNotice()}>
                  <p class="mt-3 text-sm text-emerald-700">{actionNotice()}</p>
                </Show>
                <Show when={actionError()}>
                  <p class="mt-3 text-sm text-destructive">{actionError()}</p>
                </Show>

                <Show when={isActive()}>
                  <p class="mt-3 text-xs text-muted-foreground">
                    Silme butonu güvenlik için sadece VM inactive olduktan sonra aktifleşir.
                  </p>
                </Show>
              </article>

              <div class="grid gap-4 lg:grid-cols-3">
                <JsonSection title="meta.json" payload={vmBundle()?.meta ?? {}} />
                <JsonSection title="vm.json" payload={vmBundle()?.vmConfig ?? {}} />
                <JsonSection title="hooks.json" payload={vmBundle()?.hooks ?? {}} />
              </div>
            </div>
          </Match>
        </Switch>
      </section>
    </DashboardShell>
  );
}

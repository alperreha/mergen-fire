import { A } from "@solidjs/router";
import { JSX } from "solid-js";
import { cn } from "~/lib/utils";

export type DashboardTab = "vm-list" | "vm-create";

export function DashboardShell(props: {
  activeTab: DashboardTab;
  title: string;
  subtitle: string;
  children: JSX.Element;
}) {
  return (
    <div class="min-h-screen bg-[radial-gradient(circle_at_top,_oklch(0.99_0.02_210)_0%,_oklch(0.995_0.003_220)_45%,_oklch(1_0_0)_100%)]">
      <header class="sticky top-0 z-40 border-b border-slate-200/70 bg-white/88 backdrop-blur-md">
        <div class="container py-4">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div>
              <h1 class="text-2xl font-semibold tracking-[0.08em] text-slate-800 sm:text-3xl">MERGEN</h1>
              <p class="text-xs text-muted-foreground sm:text-sm">Firecracker MicroVM Dashboard</p>
            </div>
            <nav class="flex items-center gap-2 rounded-xl border border-slate-200 bg-white/80 p-1">
              <A
                href="/?tab=vm-list"
                class={cn(
                  "rounded-lg px-3 py-2 text-sm font-medium transition",
                  props.activeTab === "vm-list"
                    ? "bg-slate-800 text-slate-50 shadow-sm"
                    : "text-slate-700 hover:bg-slate-100"
                )}
              >
                VM Listele
              </A>
              <A
                href="/?tab=vm-create"
                class={cn(
                  "rounded-lg px-3 py-2 text-sm font-medium transition",
                  props.activeTab === "vm-create"
                    ? "bg-slate-800 text-slate-50 shadow-sm"
                    : "text-slate-700 hover:bg-slate-100"
                )}
              >
                VM Oluştur
              </A>
            </nav>
          </div>
          <div class="mt-3">
            <h2 class="text-lg font-semibold text-slate-800">{props.title}</h2>
            <p class="text-sm text-muted-foreground">{props.subtitle}</p>
          </div>
        </div>
      </header>

      <main class="container py-6 pb-24">{props.children}</main>

      <footer class="sticky bottom-0 z-30 border-t border-slate-200/80 bg-white/90 backdrop-blur-md">
        <div class="container py-3 text-center text-xs text-muted-foreground">
          Mergen Dashboard • VM lifecycle + converter orchestration
        </div>
      </footer>
    </div>
  );
}

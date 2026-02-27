import { createMemo, For, Show } from "solid-js";
import { flattenObject } from "~/lib/json";

export function JsonSection(props: { title: string; payload: unknown }) {
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

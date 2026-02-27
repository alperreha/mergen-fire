import { A } from "@solidjs/router";
import { Compass, Home } from "lucide-solid";
import { Button } from "~/components/ui/button";
import { cn } from "~/lib/utils";
import { LinkLogo } from "~/components/header/link-logo";

export function Header() {
	const headerNavAnim = "animate-in fade-in slide-in-from-top-2 duration-400";

	return (
		<nav
			id="mainapp-shell-nav"
			class="shrink-0 min-h-14 w-full bg-background flex flex-row justify-between items-center px-4 border-foreground/15 border-b-1"
		>
			{/* Animate left side */}
			<div class="flex gap-2 items-center font-semibold animate-in fade-in slide-in-from-left-2 duration-200">
				<LinkLogo />
			</div>

			<div
				id="mainapp-shell-nav-right"
				class="flex flex-row gap-2 justify-end items-center"
			>
				<Button
					size="icon"
					variant="ghost"
					class={cn(headerNavAnim, "clicked-sm size-8 text-md")}
					as={A}
					href="/"
				>
					<Home strokeWidth={1.5} />
				</Button>
				<Button
					size="icon"
					variant="ghost"
					class={cn(headerNavAnim, "clicked-sm size-8 text-md")}
					as={A}
					href="/explore"
				>
					<Compass strokeWidth={1.5} />
				</Button>

				{/* <HeaderUserSection /> */}
				User Section
			</div>
		</nav>
	);
}

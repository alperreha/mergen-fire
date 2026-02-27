import { BottomNav } from "./bottom-nav";

export function Footer() {
	return (
		<footer
			id="mainapp-shell-footer"
			class="shrink-0 bg-background border-foreground/15 w-full flex items-center justify-between border-t text-center"
		>
			{/* Mobile Bottom Navigation */}
			<BottomNav />

			{/* Desktop Copyright - Hidden on mobile, shown on md+ */}
			<div class="hidden md:flex flex-row grow w-full justify-center items-center text-xs text-center gap-2 min-h-12 max-h-12">
				All rights reserved &copy; {new Date().getFullYear()}{" "}
				mergen.app
			</div>
		</footer>
	);
}

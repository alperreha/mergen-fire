import { A } from "@solidjs/router";
import { Button } from "~/components/ui/button";

export function LinkLogo() {
	return (
		<Button variant="ghost" class="p-0 gap-0 rounded-lg" as={A} href="/">
			<div class="flex flex-row gap-2 sm:mx-2 justify-center items-center">
				<img
					src="/logo-light-horizontal.svg"
					alt="mergenapp-logo"
					class="w-32 h-8"
				/>
			</div>
		</Button>
	);
}

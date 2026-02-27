import { A } from "@solidjs/router";
import { Compass, Home, User2 } from "lucide-solid";
import { Button } from "~/components/ui/button";
import { useCachedSession } from "~/lib/use-cached-session";

export function BottomNav() {
	const session = useCachedSession();

	return (
		<div class="md:hidden flex flex-row grow w-full justify-around items-center px-4 py-2 bg-background border-t border-foreground/15">
			<Button
				variant="ghost"
				size="icon"
				as={A}
				href="/"
				class="flex flex-col items-center gap-1 h-auto py-1"
			>
				<Home class="size-6" />
				<span class="text-[10px]">Home</span>
			</Button>
			<Button
				variant="ghost"
				size="icon"
				as={A}
				href="/explore"
				class="flex flex-col items-center gap-1 h-auto py-1"
			>
				<Compass class="size-6" />
				<span class="text-[10px]">Explore</span>
			</Button>
			<Button
				variant="ghost"
				size="icon"
				as={A}
				href={
					session.data ? `/profile/${session.data.user?.id}` : "/auth/sign-in"
				}
				class="flex flex-col items-center gap-1 h-auto py-1"
			>
				<User2 class="size-6" />
				<span class="text-[10px]">{session.data ? "Profile" : "Sign In"}</span>
			</Button>
		</div>
	);
}

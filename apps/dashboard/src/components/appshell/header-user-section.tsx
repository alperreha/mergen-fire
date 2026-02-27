import { A, useLocation } from "@solidjs/router";
import { DoorOpen, ExternalLink, Info, Settings2, User2 } from "lucide-solid";
import { Show } from "solid-js";
import { Avatar, AvatarFallback, AvatarImage } from "~/components/ui/avatar";
import { Button } from "~/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useCachedSession } from "~/lib/use-cached-session";
import { cn } from "~/lib/utils";

export function HeaderUserSection() {
	const session = useCachedSession();
	const location = useLocation();

	// Animation class used in appshell
	const headerNavAnim = "animate-in fade-in slide-in-from-top-2 duration-400";

	return (
		<Show
			when={session.data}
			fallback={
				<Button
					id="header-sign-in-button"
					size="sm"
					variant="outline"
					class={cn(headerNavAnim, "clicked-md")}
					as={A}
					href={`/auth/sign-in?redirect_url=${encodeURIComponent(
						location.pathname,
					)}`}
				>
					Sign In
				</Button>
			}
		>
			<DropdownMenu>
				<DropdownMenuTrigger
					class={cn("flex justify-center items-center", headerNavAnim)}
				>
					<Button variant="ghost" size="sm" class="size-8 rounded-full p-0">
						<Avatar class="size-8">
							<AvatarImage
								src={session?.data?.user?.image || "/default-avatar.png"}
								class="object-cover size-8 rounded-full"
							/>
							<AvatarFallback class="font-semibold size-8 text-xs">
								{session?.data?.user?.name
									? session?.data?.user?.name
										.split(" ")
										.map((n) => n[0])
										.join("")
										.toUpperCase()
									: "BB"}
							</AvatarFallback>
						</Avatar>
					</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent class="max-w-56 w-56 rounded-lg font-medium">
					<DropdownMenuItem
						class="rounded-md py-2 px-3 text-sm"
						as={A}
						href={`/profile/${session?.data?.user?.id}`}
					>
						<div class="flex items-center gap-2">
							<User2 class="h-4 w-4" />
							<span>Profile</span>
						</div>
					</DropdownMenuItem>
					<DropdownMenuItem
						class="rounded-md py-2 px-3"
						as={A}
						href="/settings"
					>
						<div class="flex items-center gap-2">
							<Settings2 class="h-4 w-4" />
							<span>Settings</span>
						</div>
					</DropdownMenuItem>
					<DropdownMenuSeparator />
					<DropdownMenuItem class="rounded-md py-2 px-3" as={A} href="/help">
						<div class="flex justify-between items-center w-full gap-2">
							<div class="flex items-center gap-2">
								<Info class="size-4" />
								<span>Help Center</span>
							</div>
							<ExternalLink class="size-4" />
						</div>
					</DropdownMenuItem>
					<DropdownMenuItem
						class="rounded-md py-2 px-3"
						as={A}
						href={`/auth/sign-out?redirect_url=${encodeURIComponent(
							location.pathname,
						)}`}
					>
						<div class="flex items-center gap-2">
							<DoorOpen class="h-4 w-4" />
							<span>Sign out</span>
						</div>
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</Show>
	);
}

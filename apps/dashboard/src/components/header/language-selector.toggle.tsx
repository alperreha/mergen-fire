import { Button } from "~/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { cn } from "~/lib/utils";
import { getLocale, setLocale } from "~/paraglide/runtime";

interface LanguageSelectorProps {
	class?: string;
}

export function LanguageSelectorToggle(props: LanguageSelectorProps) {
	const headerNavAnim = "animate-in fade-in slide-in-from-top-2 duration-400";

	const currentLocale = getLocale();

	const languages = [
		{ code: "en", name: "English", flag: "🇺🇸" },
		{ code: "tr", name: "Türkçe", flag: "🇹🇷" },
	];

	const currentLanguage =
		languages.find((lang) => lang.code === currentLocale) || languages[0];

	return (
		<DropdownMenu>
			<DropdownMenuTrigger>
				<Button
					size="sm"
					variant="ghost"
					class={cn(headerNavAnim, "clicked-sm text-xs font-bold", props.class)}
				>
					<div class="flex items-center gap-2">
						<span>{currentLanguage.flag}</span>
						{/* <span>{currentLanguage.name}</span> */}
					</div>
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent class="rounded-md">
				{languages.map((language) => (
					<DropdownMenuItem
						id={language.code}
						class="rounded-md py-2 px-3 cursor-pointer"
						onClick={() => setLocale(language.code as "en" | "tr")}
					>
						<div class="flex items-center gap-2">
							<span>{language.flag}</span>
							<span>{language.name}</span>
						</div>
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

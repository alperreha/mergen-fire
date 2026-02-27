import { useBeforeLeave, useLocation } from "@solidjs/router";
import type { JSX } from "solid-js";
import { Footer } from "./footer";
import { Header } from "./header";
import { cn } from "~/lib/utils";

export function AppShellComponent({ children }: { children: JSX.Element }) {
	const location = useLocation();

	const transition = (fnStartingTheSynchronousTransition: any) => {
		// In case the API is not yet supported
		// @ts-ignore document is not availible due totsconfig.json file. Update later...
		if (!document.startViewTransition) {
			return fnStartingTheSynchronousTransition();
		}

		// Transition the changes in the DOM
		// @ts-ignore document is not availible due totsconfig.json file. Update later...
		document.startViewTransition(fnStartingTheSynchronousTransition);
	};

	// docs: https://docs.solidjs.com/solid-router/reference/primitives/use-before-leave
	useBeforeLeave((e) => {
		// Stop the inmediate navigation and DOM change
		e.preventDefault();

		// Perform the action that triggers a DOM change synchronously
		transition(() => {
			e.retry(true);
		});
		// dummy 3000 delay then make transition
		// setTimeout(() => {
		// }, 5000);
	});

	return (
		<main
			id="mainapp-shell"
			class="flex flex-col max-w-screen max-h-screen h-screen w-screen overflow-x-hidden"
		>
			<Header />

			<div
				id={`mainapp-shell-content-${location.pathname?.replaceAll("/", "-")}`}
				// e.g. https://blog.logrocket.com/animating-solidjs-apps-motion-one/
				class={
					cn(
						"flex-1 max-h-full max-w-full w-full h-full overflow-y-auto overflow-x-hidden",
						"animate-in fade-in ease-in-out duration-600",
						"scrollbar-hide"
					)
				}
			>
				{children}
			</div>

			<Footer />
		</main>
	);
}

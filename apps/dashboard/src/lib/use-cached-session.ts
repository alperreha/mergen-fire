import { makePersisted } from "@solid-primitives/storage";
import { createEffect } from "solid-js";
import { createStore } from "solid-js/store";
import { createAsync } from "@solidjs/router";

// on page reload, isPending is true, and data is null, to prevent flicker, we can use a persisted store to store the data, and update it when the session is not pending

export function useCachedSession() {
	// const session = authClient.useSession();
	const session = createAsync(async () => {
		await new Promise((resolve) => setTimeout(resolve, 1000));
		return {
			isPending: false,
			data: {
				user: {
					id: "1",
					email: "[EMAIL_ADDRESS]",
					name: "Alper Reha",
					image: "https://avatars.githubusercontent.com/u/12345678?v=4",
				},
			},
			error: null,
			isRefetching: false,
		}
	});

	const [store, setStore] = makePersisted(
		createStore({ data: session()?.data }),
		{
			name: "session.data",
			storage: sessionStorage,
		},
	);

	createEffect(() => {
		if (!session()?.isPending) {
			setStore("data", session()?.data);
		}
	});

	return {
		get isPending() {
			return session()?.isPending;
		},
		get data() {
			return store.data;
		},
		get error() {
			return session()?.error;
		},
		get isRefetching() {
			return session()?.isRefetching;
		},
	};
}

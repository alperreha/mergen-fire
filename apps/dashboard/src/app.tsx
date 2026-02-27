import { Router } from "@solidjs/router";
import { FileRoutes } from "@solidjs/start/router";
import { Suspense } from "solid-js";
import { AppShellComponent } from "./components/appshell/appshell";

export default function App() {
  return (
    <Router
      root={(props) => {
        return (
          <Suspense fallback={<div>Loading...</div>}>
            <AppShellComponent>{props.children}</AppShellComponent>
          </Suspense>
        );
      }}
    >
      <FileRoutes />
    </Router>
  );
}

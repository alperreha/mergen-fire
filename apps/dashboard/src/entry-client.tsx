// @refresh reload
import { mount, StartClient } from "@solidjs/start/client";
import "@fontsource/noto-sans";
import "./app.css";

mount(() => <StartClient />, document.getElementById("app"));

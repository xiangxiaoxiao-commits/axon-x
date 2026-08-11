import "./style.css";
import { mount } from "svelte";
import App from "./App.svelte";

// Svelte 5 mounts components with mount(), not `new Component()`.
const app = mount(App, {
  target: document.getElementById("app")!,
});

export default app;

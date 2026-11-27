import "@patternfly/react-core/dist/styles/base.css";
import "@patternfly/patternfly/patternfly-addons.css";
import "./theme.scss";
// Ensure dynamic-plugin-sdk is in the MF shared scope for plugin extensions
// TODO(CVE-2023-54404): the SDK still requires Zod 3, but the first patched
// release is Zod 4.6.6; no compatible fix is available upstream yet.
import "@openshift/dynamic-plugin-sdk";
import "react/jsx-runtime";

import { createRoot } from "react-dom/client";

import { App } from "./App";
import { initThemeSettings } from "./components/Themes/ThemeDropdown";

initThemeSettings();

const root = createRoot(document.getElementById("root")!);
root.render(<App />);

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.jsx";
import { getMode } from "./colorMode.js";
import "./theme.css";

document.documentElement.dataset.theme = getMode();

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { GraphicalProvider, theme } from "components/graphical";
import "components/graphical/styles.css";
import App from "./App.jsx";
import { getMode } from "./colorMode.js";
import "./theme.css";

function Root() {
  const [mode, setMode] = useState(getMode());

  useEffect(() => {
    document.documentElement.dataset.theme = mode;
    const observer = new MutationObserver(() => {
      const current = document.documentElement.dataset.theme;
      if (current && current !== mode) setMode(current);
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, [mode]);

  const colorMode = mode === "auto"
    ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
    : (mode === "dark" ? "dark" : "light");

  return (
    <StrictMode>
      <GraphicalProvider document={theme} colorMode={colorMode}>
        <App />
      </GraphicalProvider>
    </StrictMode>
  );
}

createRoot(document.getElementById("root")).render(<Root />);

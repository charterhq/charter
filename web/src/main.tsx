import { StrictMode, lazy, Suspense } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";

// ?kit in development shows the base components; the import is dropped from the build.
const Kit = import.meta.env.DEV && new URLSearchParams(location.search).has("kit") ? lazy(() => import("./components/Kit")) : null;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {Kit ? (
      <Suspense>
        <Kit />
      </Suspense>
    ) : (
      <App />
    )}
  </StrictMode>,
);

import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";

// No StrictMode: dev double-mounting double-fires every view's fetch.
createRoot(document.getElementById("root")!).render(<App />);

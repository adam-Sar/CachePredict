import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        paper: "#f4efe6",
        paperdim: "#ece6da",
        ink: "#1c1816",
        inkmute: "#56504a",
        taupe: "#8a7f72",
        rule: "#d9d2c2",
        rust: "#b04a2a",
        rustdim: "#8c3a1f",
        amber: "#c9892d",
      },
      fontFamily: {
        display: ['"Fraunces"', "ui-serif", "Georgia", "serif"],
        sans: ['"Geist"', "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ['"JetBrains Mono"', "ui-monospace", "Menlo", "monospace"],
      },
      fontVariationSettings: {
        display: '"SOFT" 50, "WONK" 0',
      },
      animation: {
        "fade-up": "fadeUp 0.5s cubic-bezier(0.2, 0.8, 0.2, 1) both",
        "banner-in": "bannerIn 0.45s cubic-bezier(0.2, 0.9, 0.3, 1.1) both",
        "pulse-soft": "pulseSoft 1.6s ease-in-out infinite",
      },
      keyframes: {
        fadeUp: {
          "0%": { opacity: "0", transform: "translateY(8px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        bannerIn: {
          "0%": { opacity: "0", transform: "translateY(-12px) scale(0.98)" },
          "100%": { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        pulseSoft: {
          "0%, 100%": { opacity: "0.55" },
          "50%": { opacity: "1" },
        },
      },
    },
  },
  plugins: [],
} satisfies Config;
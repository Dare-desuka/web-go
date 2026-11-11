import type { Metadata } from "next";
import Shell from "@/components/Shell";
import "./globals.css";

// ponytail: layout stays a server component purely to keep `metadata` and
// the CSS import working. All markup and data fetching moved to Shell so the
// static build does not bake a catalog snapshot into every page.
export const metadata: Metadata = {
  title: "FilmGo — Nonton",
  description: "Nonton film dari penyimpanan privat.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return <Shell>{children}</Shell>;
}
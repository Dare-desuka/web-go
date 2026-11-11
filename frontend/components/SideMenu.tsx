"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";

export interface SeriesLink {
  key: string;
  label: string;
  count: number;
}

// Pornhub's #leftMenu: hamburger opens a drawer of stacked `.menuWrapper`
// sections. Their "Pornstars & Models" block is FilmGo's Series block — same
// collapsible submenu shape, ordered by updated_at (we have no view counter).
const VIDEOS = [
  { href: "/home", label: "Home" },
  { href: "/bookmarks", label: "Bookmark" },
  { href: "/history", label: "Riwayat" },
];

const ITEM =
  "flex items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm text-subtext1 hover:bg-white/10 hover:text-mauve";

function Section({
  title,
  children,
  defaultOpen = false,
  action,
}: {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
  action?: { href: string; label: string };
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="menuSection border-t border-white/10 py-2">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 px-2.5 py-1 text-[0.7rem] font-bold uppercase text-mauve hover:text-white"
      >
        <svg
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="3"
          className={`transition ${open ? "rotate-180" : ""}`}
        >
          <path d="M6 9l6 6 6-6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {title}
      </button>
      {open && <div className="mt-0.5">{children}</div>}
      {action && (
        <Link href={action.href} className={`${ITEM} font-bold text-mauve`}>
          {action.label}
        </Link>
      )}
    </div>
  );
}

export default function SideMenu({ genres, series }: { genres: string[]; series: SeriesLink[] }) {
  const [open, setOpen] = useState(false);
  const pathname = usePathname();

  useEffect(() => setOpen(false), [pathname]);

  useEffect(() => {
    document.body.style.overflow = open ? "hidden" : "";
    return () => {
      document.body.style.overflow = "";
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        aria-label="Menu"
        aria-expanded={open}
        className="-ml-1.5 shrink-0 p-1.5 text-subtext1 hover:text-mauve"
      >
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
          <path d="M3 6h18M3 12h18M3 18h18" strokeLinecap="round" />
        </svg>
      </button>

      {open && (
        <div className="fixed inset-0 z-50" role="dialog" aria-modal="true" aria-label="Menu navigasi">
          <div className="absolute inset-0 bg-black/70" onClick={() => setOpen(false)} />
          <nav
            onClick={(e) => {
              if ((e.target as HTMLElement).closest("a")) setOpen(false);
            }}
            className="absolute inset-y-0 left-0 w-[280px] max-w-[85vw] overflow-y-auto border-r border-white/10 bg-crust pb-10"
          >
            <div className="sticky top-0 z-10 flex items-center justify-between border-b border-white/10 bg-crust px-4 py-3">
              <Link href="/home" className="font-display text-lg font-black">
                Film<span className="text-mauve">Go</span>
              </Link>
              <button onClick={() => setOpen(false)} aria-label="Tutup menu" className="p-1 text-subtext1 hover:text-mauve">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                  <path d="M6 6l12 12M18 6L6 18" strokeLinecap="round" />
                </svg>
              </button>
            </div>

            {/* videosNavigation */}
            <div className="videosNavigation py-2">
              {VIDEOS.map((v) => (
                <Link key={v.label} href={v.href} className={ITEM}>
                  {v.label}
                </Link>
              ))}
            </div>

            {/* Series = Pornhub's "Pornstars & Models" block. Newest first. */}
            <Section title="Series" defaultOpen action={{ href: "/series", label: "Semua Series →" }}>
              {series.slice(0, 10).map((s) => (
                <Link key={s.key} href={`/series/${s.key}`} className={ITEM}>
                  <span className="min-w-0 flex-1 truncate">{s.label}</span>
                  <span className="shrink-0 text-[0.7rem] text-overlay0">{s.count} Eps</span>
                </Link>
              ))}
            </Section>

            {/* Top Categories — Pornhub opens this one expanded. */}
            <Section title="Kategori" defaultOpen>
              {genres.slice(0, 12).map((g) => (
                <Link key={g} href={`/home?genre=${encodeURIComponent(g)}`} className={`${ITEM} capitalize`}>
                  {g}
                </Link>
              ))}
            </Section>

            <div className="border-t border-white/10 py-2">
              <Link href="/login" className={ITEM}>
                Log In
              </Link>
            </div>
          </nav>
        </div>
      )}
    </>
  );
}
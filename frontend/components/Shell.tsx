"use client";

// Chrome (header, search, drawer, footer) plus the catalog it needs.
//
// This was the server component half of app/layout.tsx. It became a client
// component when the static build landed: the exported HTML is identical for
// every page, so fetching the catalog in the browser is both possible and
// necessary. The reason this used to run on the server — the browser could
// not always reach the API port — disappears once Go serves this same bundle
// from the API's own origin.

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import UserMenu from "@/components/UserMenu";
import SideMenu from "@/components/SideMenu";
import SearchBox from "@/components/SearchBox";
import { listMoviesClient, type Movie } from "@/lib/api";
import { groupBySeries } from "@/lib/series";

const NAV = [
  { href: "/home", label: "Home" },
  { href: "/series", label: "Series" },
  { href: "/home#katalog", label: "Katalog" },
  { href: "/bookmarks", label: "Bookmark" },
  { href: "/history", label: "Riwayat" },
];

const FOOTER = [
  { title: "FilmGo", links: NAV.slice(0, 2) },
  { title: "Koleksi", links: [{ href: "/bookmarks", label: "Bookmark" }, { href: "/history", label: "Riwayat" }] },
  { title: "Akun", links: [{ href: "/login", label: "Log In" }] },
];

export default function Shell({ children }: { children: React.ReactNode }) {
  // ponytail: catch inside listMoviesClient (it already returns [] on any
  // failure), so a dead backend degrades to "no search box", never an error
  // boundary — login and admin must still work without /api/movies.
  const [movies, setMovies] = useState<Movie[]>([]);
  useEffect(() => {
    listMoviesClient().then(setMovies);
  }, []);

  const series = useMemo(() => groupBySeries(movies), [movies]);
  const genres = useMemo(
    () =>
      [...new Set(movies.flatMap((m) => (m.genres ?? "").split(",").map((s) => s.trim()).filter(Boolean)))].sort(),
    [movies],
  );
  // Newest-touched first — our stand-in for Pornhub's view-ranked lists, since
  // we store no view counts anywhere. Singletons excluded: "1 Eps" is not a series.
  const seriesLinks = useMemo(
    () =>
      series
        .filter((g) => g.movies.length > 1)
        .map((g) => ({
          key: g.key,
          label: g.label,
          count: g.movies.length,
          updatedAt: Math.max(...g.movies.map((m) => m.updated_at)),
        }))
        .sort((a, b) => b.updatedAt - a.updatedAt),
    [series],
  );

  return (
    <html lang="id">
      <body className="min-h-screen bg-mantle font-body text-text antialiased">
        <header className="sticky top-0 z-30 border-b border-white/10 bg-crust">
          <div className="mx-auto flex max-w-[1600px] items-center gap-3 px-4 py-2.5">
            <SideMenu genres={genres} series={seriesLinks} />
            <Link href="/home" className="shrink-0 font-display text-2xl font-black tracking-tight">
              Film<span className="text-mauve">Go</span>
            </Link>
            <nav className="hidden items-center gap-6 text-[0.8rem] font-bold uppercase text-subtext1 xl:flex">
              {NAV.map((n) => (
                <Link key={n.href} href={n.href} className="hover:text-mauve">
                  {n.label}
                </Link>
              ))}
            </nav>
            {movies.length > 0 && (
              <div className="ml-auto min-w-0 max-w-md flex-1">
                <SearchBox movies={movies} series={series} />
              </div>
            )}
            <div className={movies.length > 0 ? "" : "ml-auto"}>
              <UserMenu />
            </div>
          </div>
        </header>

        <main className="mx-auto max-w-[1600px] px-4 pb-10 pt-4">{children}</main>

        <footer className="border-t border-white/10 bg-crust">
          <div className="mx-auto flex max-w-[1600px] flex-col gap-6 px-4 py-8 md:flex-row md:justify-between">
            {FOOTER.map((col) => (
              <div key={col.title}>
                <h3 className="text-xs font-bold uppercase text-subtext0">{col.title}</h3>
                <ul className="mt-2 space-y-1">
                  {col.links.map((l) => (
                    <li key={l.href + l.label}>
                      <Link href={l.href} className="text-sm text-subtext1 hover:text-mauve">
                        {l.label}
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
          <div className="border-t border-white/10 py-3 text-center text-xs text-overlay0">
            FilmGo — hanya menyimpan konten yang berhak disiarkan.
          </div>
        </footer>
      </body>
    </html>
  );
}
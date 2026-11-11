"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import type { Movie } from "@/lib/api";
import { listAll, me } from "@/lib/admin";
import { allHistory } from "@/lib/progress";
import { groupBySeries } from "@/lib/series";
import RowCard from "@/components/RowCard";
import SeriesCard from "@/components/SeriesCard";

export default function HomeApp({
  movies,
  page,
  showTop,
}: {
  movies: Movie[];
  page: number;
  showTop: boolean;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [asc, setAsc] = useState(true);
  // Admins see hidden films too (DRAFT etc.) and can play them normally.
  const [catalog, setCatalog] = useState(movies);
  // catalog must track the incoming movies prop: in the static export the
  // list arrives after mount (useState only seeds once), so without this
  // sync an anonymous visitor keeps the empty initial state forever.
  const adminRef = useRef(false);
  useEffect(() => {
    if (!adminRef.current) setCatalog(movies);
  }, [movies]);
  // Scroll to top on page change: Next.js client nav keeps scroll position,
  // so /2, /3 land at the bottom of the previous page otherwise.
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [page]);
  useEffect(() => {
    me()
      .then((u) => {
        if (u && u.role === "admin") {
          adminRef.current = true;
          listAll().then(setCatalog).catch(() => {});
        }
      })
      .catch(() => {});
  }, []);

  const sorted = useMemo(() => {
    const arr = [...catalog].sort((a, b) => a.title.localeCompare(b.title));
    return asc ? arr : arr.reverse();
  }, [catalog, asc]);

  const series = useMemo(() => groupBySeries(sorted), [sorted]);

  // Riwayat: film yang pernah ditonton, yang terakhir dulu di atas.
  const historyList = useMemo(() => {
    const at = new Map(allHistory().map((h) => [h.id, h.at]));
    return sorted.filter((m) => at.has(m.id)).sort((a, b) => at.get(b.id)! - at.get(a.id)!);
  }, [sorted]);

  const [genre, setGenre] = useState("");
  const availableGenres = useMemo(() => {
    const set = new Set<string>();
    for (const m of catalog) {
      for (const g of (m.genres ?? "").split(",").map((s) => s.trim()).filter(Boolean)) set.add(g);
    }
    return [...set].sort();
  }, [catalog]);

  // Sync genre from URL (?genre=xxx) so menu links work and URL is shareable
  useEffect(() => {
    setGenre(searchParams.get("genre") ?? "");
  }, [searchParams]);

  const setGenreWithUrl = (g: string) => {
    setGenre(g);
    const params = new URLSearchParams(searchParams.toString());
    if (g) params.set("genre", g);
    else params.delete("genre");
    const q = params.toString();
    router.push(q ? `/home?${q}` : "/home", { scroll: false });
  };

  // Page links: /home for page 1, /N after. Keeps ?genre= so filtering survives paging.
  const pageHref = (n: number) => {
    const q = searchParams.toString();
    const path = n === 1 ? "/home" : `/${n}`;
    return q ? `${path}?${q}` : path;
  };

  const filteredByGenre = useMemo(
    () => (genre ? series.filter((s) => s.movies.some((m) => (m.genres ?? "").split(",").includes(genre))) : series),
    [series, genre],
  );

  const PAGE_SIZE = 30;
  const pages = Math.max(1, Math.ceil(filteredByGenre.length / PAGE_SIZE));
  const cur = Math.min(page, pages);
  const visibleSeries = filteredByGenre.slice((cur - 1) * PAGE_SIZE, cur * PAGE_SIZE);

  // Pornhub-style numbered pagination: 1 … 4 5 6 … 12
  const pageNums = useMemo(() => {
    const out: (number | "…")[] = [];
    const push = (n: number | "…") => out.push(n);
    if (pages <= 7) {
      for (let i = 1; i <= pages; i++) push(i);
    } else {
      push(1);
      if (cur > 3) push("…");
      for (let i = Math.max(2, cur - 1); i <= Math.min(pages - 1, cur + 1); i++) push(i);
      if (cur < pages - 2) push("…");
      push(pages);
    }
    return out;
  }, [pages, cur]);

  const chip = (active: boolean) =>
    `shrink-0 rounded-sm px-3 py-1.5 text-xs font-bold uppercase ${
      active ? "bg-mauve text-crust" : "text-subtext1 hover:bg-white/10 hover:text-mauve"
    }`;

  return (
    <div>
      {/* Pornhub category sub-nav bar. */}
      <nav className="mt-4 flex items-center gap-1 overflow-x-auto border-y border-white/10 bg-crust py-1.5">
        <button onClick={() => setGenreWithUrl("")} className={chip(genre === "")}>
          Semua
        </button>
        {availableGenres.map((g) => (
          <button key={g} onClick={() => setGenreWithUrl(genre === g ? "" : g)} className={chip(genre === g)}>
            {g}
          </button>
        ))}
      </nav>

      {catalog.length === 0 ? (
        <p className="mt-8 text-sm text-subtext0">Belum ada film yang dipublikasi.</p>
      ) : (
        <>
          {showTop && historyList.length > 0 && (
            <section className="mt-6">
              <div className="mb-2 flex items-baseline justify-between gap-2">
                <h2 className="text-sm font-bold uppercase text-subtext1">
                  <span className="text-mauve">▸</span> Riwayat
                  <span className="ml-1 text-xs font-normal text-overlay0">{historyList.length}</span>
                </h2>
                {historyList.length > 4 && (
                  <Link href="/history" className="text-xs font-bold text-mauve hover:underline">
                    Riwayat ({historyList.length}) →
                  </Link>
                )}
              </div>
              <div className="grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-4">
                {historyList.slice(0, 4).map((m) => (
                  <RowCard key={m.id} movie={m} href={`/movies/${m.id}`} />
                ))}
              </div>
            </section>
          )}

          <section id="katalog" className="mt-8">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="flex items-baseline gap-2 text-sm font-bold uppercase text-subtext1">
                <span className="text-mauve">▸</span> Semua Film
                <span className="text-xs font-normal text-overlay0">{filteredByGenre.length}</span>
              </h2>
              <button
                onClick={() => setAsc((a) => !a)}
                className="rounded-sm border border-white/15 px-2.5 py-1 font-mono text-[0.7rem] hover:border-mauve hover:text-mauve"
              >
                {asc ? "A→Z" : "Z→A"}
              </button>
            </div>
            <div className="grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-4">
              {visibleSeries.map((s) => (
                <SeriesCard key={s.key} group={s} />
              ))}
            </div>
          </section>
        </>
      )}

      {pages > 1 && (
        <nav className="mt-10 flex flex-wrap items-center justify-center gap-1.5 text-sm">
          <Link
            href={pageHref(Math.max(1, cur - 1))}
            aria-disabled={cur <= 1}
            className={`px-2.5 py-1 ${cur <= 1 ? "pointer-events-none text-overlay0/50" : "hover:bg-white/10"}`}
          >
            «
          </Link>
          {pageNums.map((n, i) =>
            n === "…" ? (
              <span key={`gap${i}`} className="px-1.5 text-overlay0">
                …
              </span>
            ) : (
              <Link
                key={n}
                href={pageHref(n)}
                className={n === cur ? "rounded-sm bg-mauve px-2.5 py-1 font-bold text-crust" : "px-2.5 py-1 hover:bg-white/10"}
              >
                {n}
              </Link>
            ),
          )}
          <Link
            href={pageHref(Math.min(pages, cur + 1))}
            aria-disabled={cur >= pages}
            className={`px-2.5 py-1 ${cur >= pages ? "pointer-events-none text-overlay0/50" : "hover:bg-white/10"}`}
          >
            »
          </Link>
        </nav>
      )}
    </div>
  );
}
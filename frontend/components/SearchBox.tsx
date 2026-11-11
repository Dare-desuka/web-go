"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Movie } from "@/lib/api";
import { thumbUrl } from "@/lib/api";
import type { SeriesGroup } from "@/lib/series";

function boldMatch(text: string, q: string) {
  const i = text.toLowerCase().indexOf(q);
  if (i < 0 || !q) return text;
  return (
    <>
      {text.slice(0, i)}
      <b className="text-mauve">{text.slice(i, i + q.length)}</b>
      {text.slice(i + q.length)}
    </>
  );
}

export default function SearchBox({
  movies,
  series,
}: {
  movies: Movie[];
  series: SeriesGroup[];
}) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [hi, setHi] = useState(0);
  const wrapRef = useRef<HTMLDivElement>(null);

  const q = query.trim().toLowerCase();

  const items = useMemo(() => {
    if (!q) return [];
    const sHits = series
      .filter(
        (s) =>
          s.label.toLowerCase().includes(q) ||
          s.movies.some((m) => m.title.toLowerCase().includes(q) || (m.genres ?? "").toLowerCase().includes(q)),
      )
      .slice(0, 3)
      .map((s) => ({ kind: "series" as const, key: s.key, label: s.label, count: s.movies.length, cover: s.movies[0] }));
    const remain = 6 - sHits.length;
    const mHits = movies
      .filter((m) => m.title.toLowerCase().includes(q) || (m.genres ?? "").toLowerCase().includes(q))
      .slice(0, remain)
      .map((m) => ({ kind: "movie" as const, key: String(m.id), label: m.title, cover: m }));
    return [...sHits, ...mHits];
  }, [q, movies, series]);

  useEffect(() => {
    setHi(0);
    setOpen(!!q && items.length > 0);
  }, [q, items.length]);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, []);

  const go = (idx: number) => {
    const it = items[idx];
    if (!it) return;
    setOpen(false);
    if (it.kind === "series") router.push(`/series/${it.key}`);
    else router.push(`/movies/${it.key}`);
  };

  return (
    <div ref={wrapRef} className="relative">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (items[hi]) go(hi);
        }}
        className="flex"
      >
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onFocus={() => q && items.length && setOpen(true)}
          onKeyDown={(e) => {
            if (!open) return;
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setHi((h) => (h + 1) % items.length);
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setHi((h) => (h - 1 + items.length) % items.length);
            } else if (e.key === "Escape") {
              setOpen(false);
            }
          }}
          placeholder="Cari judul film…"
          className="min-w-0 flex-1 rounded-l-sm border border-r-0 border-white/15 bg-mantle px-3 py-1.5 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        <button
          type="submit"
          className="shrink-0 rounded-r-sm bg-mauve px-4 py-1.5 text-sm font-bold text-crust hover:brightness-110"
        >
          Cari
        </button>
      </form>

      {open && (
        <div className="absolute left-0 right-0 z-50 mt-1 max-h-80 overflow-y-auto rounded-sm border border-white/10 bg-mantle py-1 shadow-xl">
          {items.map((it, idx) => (
            <Link
              key={`${it.kind}:${it.key}`}
              href={it.kind === "series" ? `/series/${it.key}` : `/movies/${it.key}`}
              onClick={() => setOpen(false)}
              onMouseEnter={() => setHi(idx)}
              className={`flex items-center gap-3 px-3 py-2 text-sm hover:bg-white/5 ${idx === hi ? "bg-white/5" : ""}`}
            >
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={it.cover.backdrop_url ?? it.cover.poster_url ?? thumbUrl(it.cover.id)}
                alt=""
                className="h-10 w-16 shrink-0 rounded object-cover"
              />
              <span className="min-w-0 flex-1 truncate">
                {boldMatch(it.label, q)}{" "}
                {it.kind === "series" && <span className="font-mono text-xs text-overlay0">· {it.count} Eps</span>}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

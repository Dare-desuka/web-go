"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { notFound, useParams } from "next/navigation";
import { listMoviesClient, type Movie } from "@/lib/api";
import { episodeLabel, groupBySeries } from "@/lib/series";
import RowCard from "@/components/RowCard";

// ponytail: read the key from the URL via useParams(); in a static export the
// params prop is frozen to the sentinel ("_").
export default function SeriesPage() {
  const routeParams = useParams<{ key: string }>();
  const [key, setKey] = useState("");
  const [movies, setMovies] = useState<Movie[] | null>(null);

  useEffect(() => {
    const resolved = routeParams?.key;
    if (resolved && resolved !== "_") setKey(decodeURIComponent(resolved));
    else {
      const m = window.location.pathname.match(/\/series\/([^/]+)/);
      if (m) setKey(decodeURIComponent(m[1]));
    }
    listMoviesClient().then(setMovies);
  }, [routeParams]);

  const view = useMemo(() => {
    if (!movies) return null;
    const group = groupBySeries(movies).find((g) => g.key === key);
    if (!group) {
      notFound();
      return null;
    }

    // Group episodes by season number parsed from title.
    const bySeason = new Map<number, typeof group.movies>();
    for (const m of group.movies) {
      // Reuse parse to get season; fallback to 1.
      const season = (() => {
        const t = m.title.replace(/\bseason\s+0*([0-9]+)\b/gi, "S$1").replace(/\bS0+([0-9]+)\b/g, "S$1");
        const mm = t.match(/\sS([0-9]+)\s*(?:BD\s*[-:]\s*|\s*[-:]\s*)/i);
        return mm ? parseInt(mm[1], 10) : 1;
      })();
      const list = bySeason.get(season) ?? [];
      list.push(m);
      bySeason.set(season, list);
    }
    const seasons = [...bySeason.entries()].sort((a, b) => a[0] - b[0]);
    return { group, seasons };
  }, [movies, key]);

  if (!view) return null;
  const { group, seasons } = view;

  return (
    <div className="mt-2">
      <Link href="/home" className="text-xs uppercase text-subtext0 hover:text-mauve">
        ← Beranda
      </Link>
      <h1 className="mt-3 text-2xl font-bold">{group.label}</h1>
      <p className="mt-1 font-mono text-xs text-overlay0">
        {group.movies.length} episode · {seasons.length} season
      </p>
      <div className="mt-6 flex flex-col gap-8">
        {seasons.map(([num, eps]) => (
          <section key={num}>
            <h2 className="flex items-baseline gap-2 border-b border-white/10 pb-2 text-sm font-bold uppercase text-subtext1">
              <span className="text-mauve">▸</span> Season {num}
              <span className="text-xs font-normal text-overlay0">{eps.length}</span>
            </h2>
            <div className="mt-3 grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4">
              {eps.map((m) => (
                <RowCard
                  key={m.id}
                  movie={m}
                  href={`/movies/${m.id}`}
                  title={m.title}
                  meta={episodeLabel(m, seasons.length > 1)}
                />
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
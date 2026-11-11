import type { Movie } from "@/lib/api";

export interface Parsed {
  key: string;
  label: string;
  season: number;
  episode: number | null;
}

export interface SeriesGroup {
  key: string;
  label: string;
  movies: Movie[]; // sorted: season asc, episode asc, id asc
}

function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}

// Log Horizon S2 BD - 01
// Log Horizon BD - 01  (season 1)
// Nichijou BD - 01
// Foo S02 - 03 / Foo Season 2: 03 / Foo - 12 (season 1) → but plain dash without S/BD is NOT a series (MIDA-964 stays singleton).
// Anything else → singleton series of one movie.
export function parseSeries(title: string): Parsed | null {
  const t = title.trim();
  // Normalize "Season 2" → "S2", "S02" → "S2"
  const norm = t
    .replace(/\bseason\s+0*([0-9]+)\b/gi, "S$1")
    .replace(/\bS0+([0-9]+)\b/g, "S$1");
  const hasMarker = /\b(?:BD|LA)\b/i.test(t) || /\bS\d+\b/i.test(t) || /\bseason\b/i.test(t);
  if (hasMarker) {
    const m = norm.match(/^(.*?)\s*(?:S([0-9]+))?\s*(?:(?:BD|LA)\s*[-:]\s*|\s*[-:]\s*)\s*0*([0-9]+)\s*$/i);
    if (m) {
      const rawSeries = m[1].trim();
      if (rawSeries && rawSeries.length >= 2) {
        const season = m[2] ? parseInt(m[2], 10) : 1;
        const episode = parseInt(m[3], 10);
        if (Number.isFinite(season) && Number.isFinite(episode) && season >= 1 && episode >= 1) {
          return { key: slugify(rawSeries) || slugify(t), label: rawSeries, season, episode };
        }
      }
    }
  }
  // ponytail: generic "Title - NN" (space-dash-space, 1-2 digits) → S1 E NN; ceil 99, avoids year/MIDA
  const g = t.match(/^(.*)\s+-\s+0*([0-9]{1,2})\s*$/);
  if (g) {
    const rawSeries = g[1].trim();
    if (rawSeries && rawSeries.length >= 2) {
      const episode = parseInt(g[2], 10);
      if (Number.isFinite(episode) && episode >= 1 && episode <= 99) {
        return { key: slugify(rawSeries) || slugify(t), label: rawSeries, season: 1, episode };
      }
    }
  }
  return null;
}

export function episodeShort(m: Movie): string {
  const p = parseSeries(m.title);
  if (!p || p.episode === null) return m.title;
  // Single-season series (most BD): "Episode 01", multi-season: "S2 E01"
  const seasonsForKey = 0; // caller decides; keep generic
  return `E${String(p.episode).padStart(2, "0")}`;
}

export function episodeLabel(m: Movie, multiSeason: boolean): string {
  const p = parseSeries(m.title);
  if (!p) return m.title;
  // ponytail: S1 "Episode 01", S2+ "S2 Episode 01" (user wants full word, not "E")
  if (multiSeason && p.season !== 1) return `S${p.season} Episode ${String(p.episode).padStart(2, "0")}`;
  return `Episode ${String(p.episode).padStart(2, "0")}`;
}

export function groupBySeries(movies: Movie[]): SeriesGroup[] {
  const buckets = new Map<string, { label: string; movies: Array<Movie & { _s: number; _e: number | null }> }>();
  for (const m of movies) {
    const p = parseSeries(m.title);
    if (!p) {
      const k = `__single:${m.id}`;
      buckets.set(k, { label: m.title, movies: [{ ...m, _s: 1, _e: null }] });
      continue;
    }
    let b = buckets.get(p.key);
    if (!b) {
      b = { label: p.label, movies: [] };
      buckets.set(p.key, b);
    }
    b.movies.push({ ...m, _s: p.season, _e: p.episode });
  }
  const out: SeriesGroup[] = [];
  for (const [key, b] of buckets) {
    b.movies.sort((a, b2) => a._s - b2._s || (a._e ?? 0) - (b2._e ?? 0) || a.id - b2.id);
    out.push({ key, label: b.label, movies: b.movies.map(({ _s: _s, _e: _e, ...m }) => m) });
  }
  // Singletons last, series first, both alphabetical by label.
  out.sort((a, b) => {
    const sa = a.movies.length === 1 && a.key.startsWith("__single:");
    const sb = b.movies.length === 1 && b.key.startsWith("__single:");
    if (sa !== sb) return sa ? 1 : -1;
    return a.label.localeCompare(b.label);
  });
  return out;
}

export function seriesKeyForTitle(title: string, fallbackId: number): string {
  const p = parseSeries(title);
  return p ? p.key : `__single:${fallbackId}`;
}

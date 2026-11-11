// Anonymous bookmarks in localStorage (same approach as lib/progress).
// Single key holding an id array; safe to import anywhere (SSR-guarded).
import type { Movie } from "@/lib/api";

const KEY = "bookmarks";

function read(): number[] {
  if (typeof window === "undefined") return [];
  try {
    const v = JSON.parse(window.localStorage.getItem(KEY) ?? "[]");
    if (!Array.isArray(v)) return [];
    return v.filter((n) => Number.isInteger(n) && n > 0);
  } catch {
    return [];
  }
}

export function isBookmarked(movieID: number): boolean {
  return read().includes(movieID);
}

/** Toggle bookmark; returns the new state (true = bookmarked). */
export function toggleBookmark(movieID: number): boolean {
  if (typeof window === "undefined") return false;
  const cur = read();
  const next = cur.includes(movieID) ? cur.filter((n) => n !== movieID) : [...cur, movieID];
  window.localStorage.setItem(KEY, JSON.stringify(next));
  return next.includes(movieID);
}

export function removeBookmark(movieID: number): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(KEY, JSON.stringify(read().filter((n) => n !== movieID)));
}

/** Bookmarked movies in catalog order (drops ids missing from catalog). */
export function bookmarkedMovies(catalog: Movie[]): Movie[] {
  const ids = new Set(read());
  return catalog.filter((m) => ids.has(m.id));
}

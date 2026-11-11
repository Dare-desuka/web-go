// Anonymous watch progress in localStorage (PRD 28.5).
// Key: watch_progress:{film_id} -> seconds (integer).
// Safe to import anywhere: browser APIs are guarded.
import type { Movie } from "@/lib/api";

const key = (movieID: number | string) => `watch_progress:${movieID}`;
const durKey = (movieID: number | string) => `watch_dur:${movieID}`;
const watchedKey = (movieID: number | string) => `watched:${movieID}`;
const HISTORY_KEY = "watch_history";

export function getProgress(movieID: number | string): number {
  if (typeof window === "undefined") return 0;
  const v = Number(window.localStorage.getItem(key(movieID)));
  return Number.isFinite(v) && v > 0 ? Math.floor(v) : 0;
}

export function setProgress(movieID: number | string, seconds: number): void {
  if (typeof window === "undefined") return;
  if (seconds > 5) {
    window.localStorage.setItem(key(movieID), String(Math.floor(seconds)));
    touchHistory(movieID);
  }
}

export function clearProgress(movieID: number | string): void {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(key(movieID));
}

export function getDuration(movieID: number | string): number {
  if (typeof window === "undefined") return 0;
  const v = Number(window.localStorage.getItem(durKey(movieID)));
  return Number.isFinite(v) && v > 0 ? Math.floor(v) : 0;
}

export function setDuration(movieID: number | string, seconds: number): void {
  if (typeof window === "undefined") return;
  if (seconds > 0) {
    window.localStorage.setItem(durKey(movieID), String(Math.floor(seconds)));
  }
}

export function isWatched(movieID: number | string): boolean {
  if (typeof window === "undefined") return false;
  return window.localStorage.getItem(watchedKey(movieID)) === "1";
}

export function toggleWatched(movieID: number | string): boolean {
  if (typeof window === "undefined") return false;
  const k = watchedKey(movieID);
  const next = window.localStorage.getItem(k) !== "1";
  if (next) window.localStorage.setItem(k, "1");
  else window.localStorage.removeItem(k);
  return next;
}

/** Progress percent 0-100, or null when no usable position/duration. */
export function progressPct(movieID: number | string): number | null {
  const pos = getProgress(movieID);
  const dur = getDuration(movieID);
  if (pos <= 5 || dur <= 0) return null;
  return Math.min(Math.round((pos / dur) * 100), 100);
}

/** Clear all history + their positions (bookmarks and watched flags stay). */
export function clearHistory(): void {
  if (typeof window === "undefined") return;
  try {
    const raw = window.localStorage.getItem(HISTORY_KEY);
    if (raw) {
      const map = JSON.parse(raw) as Record<string, number>;
      if (typeof map === "object" && map !== null) {
        for (const id of Object.keys(map)) {
          window.localStorage.removeItem(key(id));
          window.localStorage.removeItem(durKey(id));
        }
      }
    }
  } catch {
    /* corrupted history: drop what we can */
  }
  window.localStorage.removeItem(HISTORY_KEY);
}

export function formatTime(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const mm = h > 0 ? String(m).padStart(2, "0") : String(m);
  return `${h > 0 ? `${h}:` : ""}${mm}:${String(sec).padStart(2, "0")}`;
}

/** Record last-watch time (epoch ms) for history ordering. */
export function touchHistory(movieID: number | string): void {
  if (typeof window === "undefined") return;
  try {
    const raw = window.localStorage.getItem(HISTORY_KEY);
    const map = (raw ? JSON.parse(raw) : {}) as Record<string, number>;
    if (typeof map !== "object" || map === null) return;
    map[String(movieID)] = Date.now();
    window.localStorage.setItem(HISTORY_KEY, JSON.stringify(map));
  } catch {
    /* corrupted history: ignore */
  }
}

export interface HistoryEntry {
  id: number;
  at: number;
}

/** History entries newest-first; drops ids with invalid data. */
export function allHistory(): HistoryEntry[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(HISTORY_KEY);
    if (!raw) return [];
    const map = JSON.parse(raw) as Record<string, number>;
    if (typeof map !== "object" || map === null) return [];
    return Object.entries(map)
      .filter(([id, at]) => Number.isInteger(Number(id)) && Number(id) > 0 && Number.isFinite(at))
      .map(([id, at]) => ({ id: Number(id), at }))
      .sort((a, b) => b.at - a.at);
  } catch {
    return [];
  }
}

export interface HistoryRow {
  id: number;
  title: string;
  pos: string;
  art?: string;
}

/** History rows newest-first with titles resolved (drops missing catalog ids). */
export function historyRows(catalog: Movie[], limit = 50): HistoryRow[] {
  const byId = new Map(catalog.map((m) => [m.id, m]));
  return allHistory()
    .filter((h) => byId.has(h.id))
    .slice(0, limit)
    .map((h) => {
      const m = byId.get(h.id)!;
      const pos = getProgress(h.id);
      const dur = getDuration(h.id);
      return {
        id: h.id,
        title: m.title,
        pos: pos > 5 ? `${formatTime(pos)}${dur > 0 ? ` / ${formatTime(dur)}` : ""}` : "ditonton",
        art: m.backdrop_url ?? m.poster_url,
      };
    });
}

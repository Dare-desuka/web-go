// API client for the Go backend. Server components use API_URL (server-side);
// the browser uses NEXT_PUBLIC_API_URL (video bytes stream Go -> browser,
// never through Next).
export interface Movie {
  id: number;
  title: string;
  slug: string;
  description: string;
  year?: number;
  duration_seconds?: number;
  poster_url?: string;
  backdrop_url?: string;
  genres?: string;
  video_size: number;
  video_mime: string;
  status: string;
  subtitle_count?: number;
  created_at: number;
  updated_at: number;
}

export interface Subtitle {
  id: number;
  movie_id: number;
  language: string;
  label: string;
  created_at: number;
}

export const serverBase =
  process.env.API_URL ?? "http://127.0.0.1:8080";

// ponytail: runtime host so laptop (127.0.0.1) and HP (192.168.1.10) both
// hit same host as the page — keeps session cookie same-site (Lax) and
// avoids baked NEXT_PUBLIC_API_URL mismatch. Port follows NEXT_PUBLIC_API_URL
// (backend moved 8090 → 3004); never hard-code it here again.
function runtimePublicBase(): string {
  const port = process.env.NEXT_PUBLIC_API_URL?.match(/:(\d+)\/?$/)?.[1] ?? "3004";
  if (typeof window !== "undefined" && window.location.hostname) {
    return `${window.location.protocol}//${window.location.hostname}:${port}`;
  }
  return process.env.NEXT_PUBLIC_API_URL ?? `http://127.0.0.1:${port}`;
}

export const publicBase = runtimePublicBase();

async function get<T>(path: string): Promise<T | null> {
  const res = await fetch(`${serverBase}${path}`, { cache: "no-store" });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`API ${path}: ${res.status}`);
  return (await res.json()) as T;
}

export const listMovies = () => get<Movie[]>("/api/movies").then((m) => m ?? []);
export const getMovie = (id: string) => get<Movie>(`/api/movies/${id}`);
export const listSubtitles = (movieID: string) =>
  get<Subtitle[]>(`/api/subtitles/${movieID}`).then((s) => s ?? []);

/** Browser-side catalog fetch (menu, bookmark/history pages). */
export async function listMoviesClient(): Promise<Movie[]> {
  try {
    const res = await fetch(`${publicBase}/api/movies`, { cache: "no-store" });
    if (!res.ok) {
      console.error("[listMoviesClient] HTTP", res.status, res.statusText);
      return [];
    }
    return (await res.json()) as Movie[];
  } catch (e) {
    console.error("[listMoviesClient] fetch failed:", e);
    return [];
  }
}

async function getClient<T>(path: string): Promise<T | null> {
  const res = await fetch(`${publicBase}${path}`, { cache: "no-store" });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`API ${path}: ${res.status}`);
  return (await res.json()) as T;
}

export const getMovieClient = (id: string) => getClient<Movie>(`/api/movies/${id}`);
export const listSubtitlesClient = (movieID: string) =>
  getClient<Subtitle[]>(`/api/subtitles/${movieID}`).then((s) => s ?? []);

export const streamUrl = (id: number | string) =>
  `${publicBase}/api/stream/movies/${id}`;
export const thumbUrl = (id: number | string) => `${publicBase}/api/thumbs/${id}`;
export const subtitleUrl = (movieID: number | string, subID: number) =>
  `${publicBase}/api/subtitles/${movieID}/${subID}`;
export const downloadUrl = (id: number | string) =>
  `${publicBase}/api/stream/movies/${id}?download=1`;

export function formatBytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return u === 0 ? `${n} B` : `${v.toFixed(1)} ${units[u]}`;
}

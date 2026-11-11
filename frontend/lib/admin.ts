"use client";

import type { Movie, Subtitle } from "@/lib/api";
import { publicBase } from "@/lib/api";

export interface AdminUser {
  id: number;
  username: string;
  role: string;
}

export type AdminAccount = AdminUser;

export interface PartStatus {
  part_number: number;
  size: number;
  start_offset: number;
  end_offset: number;
  status: string;
  retry_count: number;
  last_error?: string;
}

export interface UploadStatus {
  movie: Movie;
  parts: PartStatus[];
  job: {
    running: boolean;
    current_part: number;
    current_bytes: number;
    current_total: number;
    error: string;
  };
  overall: { uploaded_bytes: number; total_bytes: number; percent: number };
}

async function req(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${publicBase}${path}`, { ...init, credentials: "include" });
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`${res.status} ${body}`);
  }
  return (await res.json()) as T;
}

// ponytail: module-level cache — identity + admin catalog are re-requested by
// every page that mounts (Home/home[N]/bookmark/history), so paging admin
// refetched twice per click. Invalidated on auth change / movie mutation.
let mePromise: Promise<AdminUser | null> | null = null;
let moviesPromise: Promise<Movie[]> | null = null;
export function invalidateAuth() {
  mePromise = null;
}
export function invalidateMovies() {
  moviesPromise = null;
}

export const me = () => {
  if (!mePromise) {
    mePromise = req("/api/admin/me").then((r) => (r.ok ? json<AdminUser>(r) : null));
    mePromise.catch(() => {
      mePromise = null;
    });
  }
  return mePromise;
};
export const login = (username: string, password: string) =>
  req("/api/admin/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  })
    .then(json<AdminUser>)
    .then((u) => {
      invalidateAuth();
      return u;
    });
export const register = (username: string, password: string) =>
  req("/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  })
    .then(json<AdminUser>)
    .then((u) => {
      invalidateAuth();
      return u;
    });
export const logout = () =>
  req("/api/admin/logout", { method: "POST" }).then((r) => {
    invalidateAuth();
    return r;
  });

export const listUsers = () => req("/api/admin/users").then(json<AdminAccount[]>);
export const createUser = (data: { username: string; password: string; role: string }) =>
  req("/api/admin/users", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  }).then(json<AdminAccount>);
export const setUserRole = (id: number, role: string) =>
  req(`/api/admin/users/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ role }),
  }).then(json<AdminAccount>);
export const deleteUser = (id: number) =>
  req(`/api/admin/users/${id}`, { method: "DELETE" }).then(json<{ status: string }>);

export const listAll = () => {
  if (!moviesPromise) {
    moviesPromise = req("/api/admin/movies").then(json<Movie[]>);
    moviesPromise.catch(() => {
      moviesPromise = null;
    });
  }
  return moviesPromise;
};
export const createMovie = (data: { title?: string; description?: string; year?: number; genres?: string }) => {
  invalidateMovies();
  return req("/api/admin/movies", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  }).then(json<Movie>);
};
export const getMovie = (id: string) => req(`/api/admin/movies/${id}`).then(json<Movie>);
export const patchMovie = (id: string, data: Partial<Movie>) => {
  invalidateMovies();
  return req(`/api/admin/movies/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  }).then(json<Movie>);
};
export const deleteMovie = (id: number) => {
  invalidateMovies();
  return req(`/api/admin/movies/${id}`, { method: "DELETE" }).then(json<{ status: string }>);
};

export const startUpload = (id: number, file: File) => {
  invalidateMovies();
  const form = new FormData();
  form.append("video", file);
  return req(`/api/admin/movies/${id}/upload`, { method: "POST", body: form }).then(
    json<{ status: string }>,
  );
};

/** Same upload via XHR so the browser reports transmission progress. */
export function uploadVideo(
  id: number,
  file: File,
  onProgress?: (loaded: number, total: number) => void,
): Promise<{ status: string }> {
  invalidateMovies();
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${publicBase}/api/admin/movies/${id}/upload`);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(e.loaded, e.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText));
        } catch {
          resolve({ status: "started" });
        }
      } else {
        reject(new Error(`${xhr.status} ${xhr.responseText}`));
      }
    };
    xhr.onerror = () => reject(new Error("jaringan terputus saat mengirim"));
    const form = new FormData();
    form.append("video", file);
    xhr.send(form);
  });
}
export const uploadStatus = (id: number | string) =>
  req(`/api/admin/movies/${id}/upload`).then(json<UploadStatus>);
export const retryUpload = (id: number) =>
  req(`/api/admin/movies/${id}/retry`, { method: "POST" }).then(json<{ status: string }>);

export type AdminSubtitle = Subtitle;

export const uploadSubtitle = (id: number, file: File, language: string, label: string) => {
  invalidateMovies();
  const form = new FormData();
  form.append("file", file);
  form.append("language", language);
  form.append("label", label);
  return req(`/api/admin/movies/${id}/subtitles`, { method: "POST", body: form }).then(
    json<AdminSubtitle>,
  );
};
export const deleteSubtitle = (subID: number) => {
  invalidateMovies();
  return req(`/api/admin/subtitles/${subID}`, { method: "DELETE" }).then(json<{ status: string }>);
};
export const listSubtitlesAdmin = (id: number | string) =>
  req(`/api/admin/movies/${id}/subtitles`).then(json<AdminSubtitle[]>);

export const uploadPoster = (id: number, kind: "poster" | "backdrop", file: File) => {
  invalidateMovies();
  const form = new FormData();
  form.append("file", file);
  return req(`/api/admin/movies/${id}/${kind}`, { method: "POST", body: form }).then(json<Movie>);
};

export const publishMovie = (id: number) => setPublished(id, true);
export const unpublishMovie = (id: number) => setPublished(id, false);

function setPublished(id: number, on: boolean) {
  invalidateMovies();
  return req(`/api/admin/movies/${id}/${on ? "publish" : "unpublish"}`, { method: "POST" }).then(
    json<Movie>,
  );
}

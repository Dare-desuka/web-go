"use client";

import { useEffect, useRef, useState } from "react";
import type { Movie, Subtitle } from "@/lib/api";
import { streamUrl, subtitleUrl } from "@/lib/api";
import { clearProgress, formatTime, getProgress, setDuration, setProgress } from "@/lib/progress";

export default function Player({
  movie,
  subtitles,
}: {
  movie: Movie;
  subtitles: Subtitle[];
}) {
  const ref = useRef<HTMLVideoElement>(null);
  const [resumeAt, setResumeAt] = useState<number | null>(null);
  const lastSaved = useRef(0);

  useEffect(() => {
    const v = ref.current;
    if (!v) return;
    const onLoaded = () => {
      if (v.duration > 0) setDuration(movie.id, v.duration);
      const saved = getProgress(movie.id);
      if (saved > 10 && v.duration > 0 && saved < v.duration - 10) {
        setResumeAt(saved);
      }
    };
    const onTime = () => {
      if (v.currentTime - lastSaved.current > 5) {
        lastSaved.current = v.currentTime;
        setProgress(movie.id, v.currentTime);
      }
    };
    const onEnded = () => clearProgress(movie.id);
    v.addEventListener("loadedmetadata", onLoaded);
    v.addEventListener("timeupdate", onTime);
    v.addEventListener("ended", onEnded);
    return () => {
      v.removeEventListener("loadedmetadata", onLoaded);
      v.removeEventListener("timeupdate", onTime);
      v.removeEventListener("ended", onEnded);
    };
  }, [movie.id]);

  return (
    <div>
      {resumeAt !== null && (
        <div className="mb-3 flex items-center justify-between rounded-sm border border-surface0 bg-base px-4 py-3">
          <p className="text-sm">
            Lanjutkan dari <span className="font-semibold">{formatTime(resumeAt)}</span>?
          </p>
          <div className="flex gap-2">
            <button
              className="rounded-sm bg-mauve px-3 py-1.5 text-sm font-medium text-crust hover:brightness-110"
              onClick={() => {
                if (ref.current) ref.current.currentTime = resumeAt;
                setResumeAt(null);
              }}
            >
              Lanjutkan
            </button>
            <button
              className="rounded-sm border border-surface0 px-3 py-1.5 text-sm hover:bg-surface0"
              onClick={() => setResumeAt(null)}
            >
              Dari awal
            </button>
          </div>
        </div>
      )}
      <video
        ref={ref}
        className="aspect-video w-full rounded-sm bg-black"
        controls
        playsInline
        preload="metadata"
        crossOrigin="use-credentials"
        src={streamUrl(movie.id)}
      >
        {subtitles.map((s, i) => (
          <track
            key={s.id}
            kind="subtitles"
            srcLang={s.language}
            label={s.label}
            src={subtitleUrl(movie.id, s.id)}
            default={i === 0}
            // crossOrigin missing from React's <track> types; spread bypasses
            // the check — the DOM attribute is valid and makes the .vtt
            // fetch carry the session cookie.
            {...{ crossOrigin: "anonymous" }}
          />
        ))}
      </video>
    </div>
  );
}

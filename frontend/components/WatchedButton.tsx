"use client";

import { useState } from "react";
import { isWatched, toggleWatched } from "@/lib/progress";

export default function WatchedButton({ movieID }: { movieID: number }) {
  const [watched, setWatched] = useState(() => isWatched(movieID));
  return (
    <button
      onClick={() => setWatched(toggleWatched(movieID))}
      className={`rounded-sm px-4 py-2 text-sm font-medium ${
        watched
          ? "bg-sapphire font-bold text-crust hover:brightness-110"
          : "border border-white/10 hover:bg-white/10"
      }`}
    >
      {watched ? "✓ Sudah ditonton" : "Tandai sudah ditonton"}
    </button>
  );
}

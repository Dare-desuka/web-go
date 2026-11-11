"use client";

import { useState } from "react";
import { isBookmarked, toggleBookmark } from "@/lib/bookmark";

export default function BookmarkButton({ movieID }: { movieID: number }) {
  const [marked, setMarked] = useState(() => isBookmarked(movieID));
  return (
    <button
      onClick={() => setMarked(toggleBookmark(movieID))}
      className={`rounded-sm px-4 py-2 text-sm font-medium ${
        marked
          ? "bg-mauve font-bold text-crust hover:brightness-110"
          : "border border-white/10 hover:bg-white/10"
      }`}
    >
      {marked ? "★ Bookmark" : "☆ Bookmark"}
    </button>
  );
}

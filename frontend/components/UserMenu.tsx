"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { logout, me, type AdminUser } from "@/lib/admin";

// Inline account control. Was a "☰ Menu" popup duplicating the nav, with a
// hardcoded genre list (romance — not in the catalog). Navigation now lives in
// the header + SideMenu; this only keeps who-you-are and the logout, which
// have nowhere else to go.
export default function UserMenu() {
  const router = useRouter();
  const [user, setUser] = useState<AdminUser | null | undefined>(undefined);

  useEffect(() => {
    me()
      .then(setUser)
      .catch(() => setUser(null));
  }, []);

  if (user === undefined) return <span className="h-7 w-16" />;

  if (!user) {
    return (
      <Link href="/login" className="shrink-0 rounded-sm bg-mauve px-3 py-1.5 text-sm font-bold text-crust hover:brightness-110">
        Masuk
      </Link>
    );
  }

  return (
    <div className="flex shrink-0 items-center gap-2">
      {user.role === "admin" && (
        <Link href="/admin" className="rounded-sm border border-white/15 px-2.5 py-1 text-xs font-bold uppercase hover:border-mauve hover:text-mauve">
          Admin
        </Link>
      )}
      <span className="hidden max-w-24 truncate text-sm text-subtext1 sm:inline" title={user.username}>
        {user.username}
      </span>
      <button
        onClick={() =>
          logout().then(() => {
            setUser(null);
            router.refresh();
          })
        }
        className="rounded-sm border border-white/15 px-2.5 py-1 text-xs font-bold uppercase hover:border-mauve hover:text-mauve"
      >
        Keluar
      </button>
    </div>
  );
}
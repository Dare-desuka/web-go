"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { me, type AdminUser } from "@/lib/admin";

/** Gates admin pages: redirects to login when unauthenticated. */
export default function AdminGuard({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [user, setUser] = useState<AdminUser | null>(null);

  useEffect(() => {
    me().then((u) => {
      if (!u) router.replace("/login");
      else setUser(u);
    });
  }, [router]);

  if (!user) return <p className="mt-10 text-subtext0">Memeriksa sesi…</p>;
  return <>{children}</>;
}

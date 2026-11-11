"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { login, register } from "@/lib/admin";

export default function LoginPage() {
  const router = useRouter();
  const [tab, setTab] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const friendly = (e: unknown, notFound: string) => {
    const msg = e instanceof Error ? e.message : "";
    if (msg.startsWith("401")) return notFound;
    if (msg.startsWith("409")) return "Username sudah dipakai.";
    if (msg.startsWith("429")) return "Terlalu sering, coba lagi nanti.";
    return `Gagal (${msg || "jaringan/server tidak terjangkau"}).`;
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (tab === "login") {
        await login(username.trim(), password);
      } else {
        await register(username.trim(), password);
      }
      router.replace("/");
      router.refresh();
    } catch (err) {
      setError(friendly(err, "Username atau password salah."));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto mt-16 max-w-sm rounded-sm border border-white/10 bg-mantle p-6">
      <div className="flex gap-2">
        {(["login", "register"] as const).map((t) => (
          <button
            key={t}
            onClick={() => {
              setTab(t);
              setError("");
            }}
            className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-bold ${
              tab === t ? "bg-mauve text-crust" : "text-subtext0 hover:bg-white/5"
            }`}
          >
            {t === "login" ? "Masuk" : "Buat Akun"}
          </button>
        ))}
      </div>
      <form onSubmit={submit} className="mt-4 flex flex-col gap-3">
        <input
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="Username"
          autoComplete="username"
          className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder={tab === "login" ? "Password" : "Password (min 8 karakter)"}
          autoComplete={tab === "login" ? "current-password" : "new-password"}
          className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        {error && <p className="text-sm text-softred">{error}</p>}
        <button
          type="submit"
          disabled={busy}
          className="rounded-sm bg-mauve px-4 py-2 font-bold text-crust hover:brightness-110 disabled:opacity-50"
        >
          {busy ? "…" : tab === "login" ? "Masuk" : "Daftar"}
        </button>
      </form>
      <p className="mt-3 text-xs text-overlay0">
        Akun baru berrole user. Admin bisa menaikkan ke admin dari dasbor.
      </p>
    </div>
  );
}

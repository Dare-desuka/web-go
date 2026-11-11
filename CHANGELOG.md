# Changelog
## 2026-10-19 — Static export: Go melayani frontend, tanpa Node
- Alasan: STB S905X (2 GB). `next start` di port 3003 memakan 300-500 MB; export statis dilayani Go sendiri di port API, jadi hemat ~400 MB dan frontend ikut same-origin.
- **Server component → client component.** `layout.tsx`, `home`, `[page]`, `series`, `series/[key]`, `bookmarks`, `history` sekarang ambil data di browser. Komentarnya dulu menyebut server fetch itu wajib karena browser tak selalu bisa reach port API; split dua-port itu hilang bersama Next, jadi fetch same-origin selalu bisa. Bookmark/history tetap dapat film DRAFT milik admin lewat `listAll()` yang sudah ada di `BookmarkList`/`HistoryList`.
- **`components/Shell.tsx` (baru)** memegang header, search, drawer, footer. `layout.tsx` tetap server component hanya untuk `metadata` + import CSS.
- **Route dinamis jadi satu shell.** `movies/[id]`, `series/[key]`, `[page]`, `admin/movies/[id]` = client component yang baca param dari URL, jadi isinya sama untuk semua param. Masing-masing dapat wrapper server `generateStaticParams` sentinel (`_`, `2`) karena Next melarang `generateStaticParams` di file `"use client"`. View dipindah ke `components/{MovieDetailView,SeriesPageView,PaginatedHomeView,AdminMovieView}.tsx`.
- **`backend/app/static.go` (baru)** menyajikan export itu: `/api/*` tetap ke mux, sisanya dilayani file statis, dengan rewrite sentinel (`/movies/7` → `movies/_.html`, `/9` → `2.html`) untuk RSC payload client-nav juga. Pakai `http.ServeContent`, bukan `http.FileServer` — FileServer me-redirect `/index.html` ke `./`.
- **`backend/app/app.go` (baru)**: pemindahan isi `cmd/server/main.go` ke package non-internal supaya binary `suite` bisa mengimpornya (`internal/` tidak bisa diimpor dari modul lain). `cmd/server/main.go` jadi pembungkus tipis; perilakunya tidak berubah. `tmp/` dan `data/` sekarang diturunkan dari `DATABASE_PATH`, jadi tiap service bisa punya pohonnya sendiri.
- **`FRONTEND_DIR`** menyalakan penyajian frontend. Kosong = perilaku lama (API saja, Next di port sendiri).
- **`config.LoadFrom(getenv)`**: `Load()` tetap pembungkus `os.Getenv`, jadi test lama tidak berubah. Dibutuhkan karena web-go dan music-go sama-sama memakai `DATABASE_PATH`, `SERVER_PORT`, dan `TELEGRAM_*`.
- Files: `frontend/{next.config.ts,components/{Shell,MovieDetailView,SeriesPageView,PaginatedHomeView,AdminMovieView}.tsx,app/{layout,home/page,[page]/page,series/page,series/[key]/page,bookmarks/page,history/page,movies/[id]/page,admin/movies/[id]/page}.tsx}`, `backend/{app/app.go,app/static.go,app/static_test.go,cmd/server/main.go,internal/config/config.go}`.
- Tests: `go test ./backend/...` semua ok (`app` baru: 8 test — export, RSC payload, fallback paginasi, 404, api passthrough, path traversal, cache header, sentinel). `go build` + `go vet` bersih. `npm run build` sukses, 15 route → `out/` 2.4 MB. Smoke test lokal (suite, 1 proses): `/`, `/home`, `/series`, `/movies/7`, `/series/log-horizon`, `/admin/movies/3`, `/2`, `/9` → 200; `/1` dan `/nope` → 404; `?_rsc=x` → payload; `/api/movies` → 200; chunk `_next/static/*` → 200.
- **Catatan:** `FRONTEND_URL` di `.env` diubah dari port 3003 ke 3004 (frontend sudah satu origin dengan API). `next start` di 3003 tidak dipakai lagi; `gorun` sekarang menjalankan binary `suite`.

## 2026-10-16 — Menu drawer: "Semua Film" → "Home", brand drawer jadi link
- Item drawer "Semua Film" diganti label "Home" (href `/home`). Brand di header sudah link `/home`; brand di dalam drawer tadinya `<span>` mati → sekarang `<Link href="/home">`.
- Klik terasa mati kalau sudah di `/home` (route sama → drawer tak menutup). Tambah `onClick` di `<nav>` drawer: kalau yang diklik anchor → `setOpen(false)`. Jadi tap "Home"/"Bookmark"/dll selalu responsif.
- Files: `frontend/components/SideMenu.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 12 route. Chrome (isolated, prod 3003): drawer buka → item `["FilmGo","Home","Bookmark","Riwayat"]`, brand href `/home`; klik "Home" saat di `/home` → drawer tertutup, tetap `/home`; dari `/2` → `/home`.
- **Catatan:** build sempat serve chunk 500 gara-gara `.next` sisa build tumpang-tindih + restart gagal (EADDRINUSE). Dibersihkan `rm -rf .next` → build ulang → start (pid 1132426), semua chunk 200.
- Heading seksi katalog di home masih "Semua Film" (bukan menu) — belum diubah.

## 2026-10-16 — Pindah halaman home lebih mulus: skeleton + cache admin
- Penyebab jerky: tiap pindah `/home`↔`/N` = navigasi server tanpa `loading.tsx` (layar beku sampai RSC datang), dan `HomeApp` remount → `me()`+`listAll()` jalan ulang tiap klik (grid lompat dari katalog READY ke katalog admin penuh).
- **A. Skeleton:** `components/HomeSkeleton.tsx` (baru) + `app/home/loading.tsx`, `app/[page]/loading.tsx` (baru) — feedback instan saat navigasi klien nunggu fetch; juga mengaktifkan prefetch route dinamis.
- **C. Cache modul:** `lib/admin.ts` — `me()` & `listAll()` di-cache di scope modul (promise), di-reset saat error. `invalidateAuth()` dipanggil di `login`/`register`/`logout`; `invalidateMovies()` dipanggil di semua mutasi film (`createMovie`, `patchMovie`, `deleteMovie`, `publish/unpublish`, `uploadPoster`, `startUpload`, `uploadVideo`, `uploadSubtitle`, `deleteSubtitle`) → admin nggak refetch 2×/klik, dan data tetap segar setelah edit.
- Files: `frontend/components/HomeSkeleton.tsx`, `frontend/app/{home/loading.tsx,[page]/loading.tsx}`, `frontend/lib/admin.ts`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 12 route. Prod `next start` restart (pid 1056558); `/home`,`/2` 200. Chrome Slow 3G (isolated): klien-nav `/2`→`/home` berhasil tanpa regresi.
- **Catatan:** skeleton cuma kelihatan saat render server lambat/navigasi klien; di lokal render <50ms jadi HTML awal nggak inline fallback (normal Next). Cache `listAll` per-tab (bukan cross-tab).

## 2026-10-16 — Home paginasi pakai URL `/home`, `/2`, `/3` + kembali ke halaman asal
- Paginasi home dulu cuma state client (`useState`) → URL nggak berubah, refresh/share/back kehilangan halaman. Sekarang page 1 = `/home`, page 2+ = `/2`, `/3`, …; `/` `redirect("/home")`.
- `app/[page]/page.tsx` (baru): route dinamis root, `Number.isInteger(n) && n >= 2` atau `notFound()`. Route statis (`/movies`, `/series`, …) tetap menang atas `[page]`. `app/page.tsx` jadi redirect; `app/home/page.tsx` (baru) = page 1.
- `components/HomeApp.tsx`: prop `page` + `showTop`. Hapus `useState` page; `cur` dari prop. Seksi **Riwayat** (yang dimaksud user "hero") hanya di `/home` (`showTop`); page 2+ = grid + paginasi. Tombol pager jadi `<Link>` (`n===1 ? /home : /N`) dan `?genre=` ikut terbawa.
- `app/movies/[id]/page.tsx`: `← Beranda` (hardcode `/`) jadi tombol `← Kembali` → `window.history.length > 1 ? router.back() : router.push("/home")`. Dari `/2` balik ke `/2`, bukan home.
- Link `/` → `/home`: `layout.tsx` (logo + NAV Home/Katalog), `SideMenu.tsx` (Semua Film + genre), `series/page.tsx`, `series/[key]/page.tsx`, chip genre di `movies/[id]`.
- Files: `frontend/app/{page.tsx,home/page.tsx,[page]/page.tsx,layout.tsx,series/page.tsx,series/[key]/page.tsx,movies/[id]/page.tsx}`, `frontend/components/{HomeApp.tsx,SideMenu.tsx}`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 12 route (termasuk `ƒ /[page]`, `ƒ /home`). Prod `next start` restart (pid 1051097). HTTP: `/`→200 (redirect), `/home`,`/2`,`/3`→200; `/foo`,`/0`,`/1`→404. Pager href cek: `/home` → `/2,/3,/4`; `/2` → `/home,/3,/4`; `/home?genre=drama` → `/2?genre=drama` dll.
- Live E2E Chrome (isolated context, prod 3003): `localStorage.watch_history={9,172}` → `/home` punya heading `▸ Riwayat2`, `/2` tidak (showTop gate OK). Klik film dari `/2` → `/movies/8` → `← Kembali` → balik `/2` (bukan home).
- **Catatan:** PAGE_SIZE 30 sementara diturunkan ke 3 di dev untuk memicu pager (katalog anon cuma 12 series); sudah dikembalikan ke 30 sebelum build final. Fallback `history.length > 1` boleh salah anggap di tab automation yang mulai dari `about:blank`, tapi di tab user normal benar.

## 2026-10-12 — `/bookmarks` dibenerin, bug yang sama kayak `/history`
- Dua bug identik kayak yang kemarin di `/history`: `listMoviesClient()` (fetch browser ke `:3004`, `catch { return [] }` → diam-diam kosong) + katalog READY-only → bookmark film DRAFT ke-filter habis.
- `app/bookmarks/page.tsx` → server component `await listMovies()`; `components/BookmarkList.tsx` (baru, client) nerima `movies` sebagai props + `me()`/`listAll()` buat admin. Tombol "Hapus" sekarang panggil `bookmarkedMovies(catalog)` langsung (dulu `load()`).
- Files: `frontend/app/bookmarks/page.tsx`, `frontend/components/BookmarkList.tsx` (baru).
- Tests: `npx tsc --noEmit` exit 0; `npm run build` sukses; restart `next start` → `pid 823379`; `/`, `/bookmarks`, `/history` semua 200 di `192.168.1.10:3003`. Live E2E origin LAN (anon): seed `[9:READY, 172:READY, 10:DRAFT]` → 2 baris (Sakamoto Days, District 13), id 10 difilter; klik "Hapus" → 1 baris, `localStorage.bookmarks` = `[9,10]`. Jalur non-admin nggak berubah.
- **Belum terverifikasi:** cabang admin (`listAll()`) cuma dicek paritas kode — nggak bisa E2E tanpa password admin.
- Sisa satu `listMoviesClient()`: `app/movies/[id]/page.tsx:52` (daftar "film terkait"). Dampak rendah, dibiarkan.

## 2026-10-12 — `/history` dan bar "Riwayat" di Home/rules katalog sama
- Gejala: HP home nulis "Riwayat 12", `/history` kosong. Laptop 3 entri → 2 baris.
- Root cause: dua halaman lihat dua katalog berbeda. `HomeApp` (`components/HomeApp.tsx:19-25`) kalau admin → `listAll()` (174 film, termasuk 46 DRAFT); `/history` cuma `listMovies()` dari server (128, **READY saja**). `historyRows()` filter `byId.has()` (`lib/progress.ts:137`) jadi semua entri film DRAFT dibuang. Laptop: 1 dari 3 DRAFT. HP: 12 dari 12 DRAFT.
- Fix: `components/HistoryList.tsx` pakai aturan Home — `useState(movies)` + `useEffect` `me().then(u => u.role === "admin" && listAll().then(setCatalog))`. Tombol "Hapus riwayat" sekarang panggil `historyRows(catalog, 500)` langsung.
- Files: `frontend/components/HistoryList.tsx`. `app/history/page.tsx` nggak berubah.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` sukses (BUILD_ID `dr2awvar2ol_IYqxt5qpP`); restart `next start` → `pid 815668`, `/` 200 + `/history` 200 di `192.168.1.10:3003`. Live E2E origin LAN (anon): seed `{9:READY, 172:READY, 10:DRAFT}` → 2 baris (District 13, Sakamoto Days), id 10 difilter — jalur non-admin nggak berubah.
- **Belum terverifikasi:** cabang admin (`me()` → `listAll()`) cuma dicek paritas kode sama HomeApp, nggak bisa E2E tanpa password admin. Perlu konfirmasi user: login admin → `/history` harus nunjuk jumlah yang sama dengan bar Riwayat di home.
- **Known gap (belum dibenerin, pola sama):** `app/bookmarks/page.tsx:15` masih `listMoviesClient()` + nggak bisa lihat film DRAFT → bug yang sama, beda halaman.

## 2026-10-12 — `/history` nggak diam-diam kosong di HP
- Gejala: home nampilin "Riwayat (N)" dengan N > 4, tapi `/history` → "Belum ada riwayat tontonan."
- Root cause: `/history` ambil katalog dari **browser** (`listMoviesClient()` → `http://<ip>:3004/api/movies`), sedangkan Home ambil dari **server** (`listMovies()` → `API_URL`, server-to-server). `listMoviesClient()` menelan error diam-diam (`catch { return [] }`, `lib/api.ts:66`) → fetch browser gagal = daftar kosong, bukan error. Katalog kosong → `historyRows()` filter `byId.has()` membuang semua entri.
- Fix: `app/history/page.tsx` jadi **server component** (`await listMovies()`, path yang sama dengan Home) → `components/HistoryList.tsx` (baru, client) nerima `movies: Movie[]` sebagai props, nol fetch browser.
- Files: `frontend/app/history/page.tsx`, `frontend/components/HistoryList.tsx` (baru).
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 10/10 route; `next start` restart (`pid 793783`). Live E2E di origin LAN `http://192.168.1.10:3003/history` (path yang sama dengan HP): seed 4 entri `watch_history` → 3 baris tampil + "Hapus riwayat"; entri ke-4 (id 34) nggak ada di katalog → difilter, sesuai `historyRows()`. `Belum ada riwayat` = false.
- **Known gap (belum dibenerin, pola sama):** `app/bookmarks/page.tsx:15` dan `app/movies/[id]/page.tsx:52` masih `listMoviesClient()` → punya gejala identik kalau client nggak bisa nabrak `:3004`.
- Catatan: `:3004` itu memang bener — `:3003` Next (web), `:3004` Go API. Byte video streams Go → browser langsung, ngelewatin Next (`lib/api.ts:1-3`).

## 2026-10-12 — Alias `gorun` beneran rebuild (bukan "masih sama aja")
- Gejala: edit frontend/backend nggak kelihatan setelah `gorun`. Root cause di `~/.bashrc:82`, 2 bug:
  1. `if [ -x ./bin/web-go-server ]; then …cached binary…; else go build …; fi` → karena binary ada, build **dilewati**. `bin/web-go-server` terakhir di-build 1 Okt.
  2. `(cd frontend && PORT=3003 nohup npm run start …)` → `next start` = produksi, cuma baca `.next/`, nggak pernah build. `.next/BUILD_ID` masih 4 Okt.
- Fix: `go build` tanpa kondisi; frontend jadi `npm run build >/tmp/web-go-frontend-build.log 2>&1 && PORT=3003 nohup npm run start`.
- Trade-off: `gorun` lambat ±10-20s (build 10 route Next). Pakai `npm run dev` kalau lagi ngoprek, jangan pas produksi.
- `/history` kosong **bukan** bug: pipeline tes OK (main 12s → `watch_history={"9":…}` → `/history` nampilin "District 13 Ultimatum 2009").localStorage itu per-origin — buka `/history` di origin yang sama dengan tempat nonton, bukan `localhost:3003` vs `192.168.1.10:3003` campur.
- Files: `~/.bashrc` (di luar repo). Nol perubahan source project.
- Tests: `go build -o ./bin/web-go-server ./backend/cmd/server` exit 0, timestamp `12:52` (sebelumnya 1 Okt); `npm run build` 10/10 route.

## 2026-10-12 — "Lanjutkan Menonton" berubah jadi "Riwayat"
- `SideMenu.tsx`: item "Lanjutkan Menonton" dihapus. Sisa 3: Semua Film / Bookmark / Riwayat.
- `HomeApp.tsx`: mode `?lanjutkan=1` dibongkar (paramnya nggak dipakai siapa lagi) — nav kategori, seksi "Semua Film", dan paginasi nggak perlu di-conditional lagi.
- `HomeApp.tsx`: `continueList` → `historyList`, sekarang benar-benar riwayat: semua film yang id-nya ada di `allHistory()`, urut terakhir-ditonton di atas, bukan cuma yang belum selesai (`!isWatched && progress > 5` filter dibuang).
- Judul seksi home `▸ Riwayat`, tetap 4 film (`slice(0, 4)`), link `Riwayat (N) →` ngarah ke `/history`.
- `app/history/page.tsx`: `historyRows(catalog)` → `historyRows(catalog, 500)` (default 50, user minta semua).
- Files: `frontend/components/{HomeApp,SideMenu}.tsx`, `frontend/app/history/page.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `grep -rn lanjutkan` bersih.

## 2026-10-04 — Admin/login/bookmark/history diseragamkan (ponytail)
- Radius: `rounded-md`(58) + `rounded-xl`(12) + `rounded-lg`(2) + `rounded-full` pill filter(3) → `rounded-sm` semuanya, biar sama dengan halaman publik yang sudah `rounded-sm`. `rounded-full` **dipertahankan** di 2 titik: lingkaran play overlay `RowCard`/`SeriesCard` — itu memang bulat.
- `font-display` (serif) dibuang dari 6 judul halaman (`/series/[key]`, `/bookmarks`, `/history`, `/movies/[id]`, `/admin`, `/admin/movies/[id]`) — sisa token Catppuccin yang bikin `/bookmarks` kelihatan beda dari `/`. **Dipertahankan** di 2 tempat: wordmark logo di `layout.tsx` dan header drawer `SideMenu.tsx` — itu keputusan brand, bukan sisa.
- Palet nggak disentuh: token sudah global, admin otomatis ikut `#000`/`#0e0e0e`/`#f90`.
- Files: `frontend/app/{admin/page.tsx,admin/movies/[id]/page.tsx,login/page.tsx,bookmarks/page.tsx,history/page.tsx,series/[key]/page.tsx,movies/[id]/page.tsx}`, `frontend/components/{Player,BookmarkButton,WatchedButton}.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 10/10 route. `/`, `/login`, `/bookmarks`, `/history`, `/series`, `/movies/9`, `/admin` semua 200. Screenshot `/login` + `/bookmarks` konsisten (judul sans, kartu siku, tombol `#f90`).
- **Known gap:** `/admin` dan `/admin/movies/[id]` **belum diverifikasi visual** — `AdminGuard` redirect ke `/login` tanpa sesi, jadi perubahan class di sana baru dicek lewat `tsc` + `grep`, bukan screenshot. Perlu cek manual setelah login.

## 2026-10-04 — Palet dari CSS asli pornhub.com, bukan tebakan (ponytail)
- Tarik `ei.phncdn.com/www-static/css/generated-header.css` (725KB) + `global-backgrounds.css`, hitung frekuensi hex. Nilai yang dipakai persis dari sana; deklarasi aslinya ditulis sebagai komentar di `globals.css` biar bisa dicek ulang.
- **Dua kesalahan lama ketahuan:**
  1. **Accent salah.** Aslinya `#f90` (118× pemakaian; tombol & nav aktif `background:#f90;color:#000`), bukan `#ff6f00` hasil tebakan gue. Ada juga `#ff9000` (62×) untuk varian lain — `#f90` yang dipakai buat tombol/tautan.
  2. **Urutan terang-gelap dibalik.** `body{background:#000}` tapi `#header #headerWrapper{background-color:#0e0e0e}` — halamannya **lebih hitam** dari bar header. Gue sebelumnya bikin sebaliknya (body `#1b1b1b`, header `#000`); itu sebab utamanya kelihatan "nggak mirip".
- Token final: `crust #0e0e0e` (header/footer), `mantle #000` (halaman), `base #1f1f1f`, `surface0 #2f2f2f` (hairline), `surface1 #363636`, `text #fff`, `subtext0 #c6c6c6`, `subtext1 #d9d9d9`, `overlay0 #969696`, `mauve #f90`. `lavender` → `#c6c6c6` (nggak ada ungu di palet Pornhub; menyisakan sisa token Catppuccin).
- `border-white/10`/`bg-white/5` di atas hitam ≈ `#1a1a1a`, dekat `#151515` milik mereka — **111 call site, sengaja nggak disentuh**; selisihnya nggak kelihatan dan ngoprek 111 baris cuma demi itu.
- Files: `frontend/app/globals.css`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 10/10 route. `/` 200; screenshot: halaman hitam pekat, header `#0e0e0e`, tombol oranye `#f90` dengan teks gelap.

## 2026-10-04 — Popup menu lama dibuang, logout jadi inline (ponytail)
- Akar masalah "ada 2 menu": `UserMenu` lama punya button berlabel **"☰ Menu"** di header, berdampingan dengan hamburger `SideMenu` yang baru. Dua-duanya tombol menu di bar yang sama.
- `components/UserMenu.tsx`: popup-nya **dihapus total**, jadi kontrol akun inline — logout → tombol oranye "Masuk"; login → `username` + "ADMIN" + "Keluar". Navigasi (bookmark/history/genre) nggak dihapus paksa, cuma pindah ownership: nav atas + `SideMenu`.
- Genre di popup lama **hardcode** `["comedy","drama","romance","fantasy","crime","horror"]` dan sudah basi — `romance` nggak ada di katalog. Sekarang SideMenu取 dari `movies` beneran.
- **Nggak hilang fitur**: "Keluar" (logout) dan "Dasbor Admin" tetap ada — keduanya nggak punya tempat lain selain kontrol akun ini. Kalau `UserMenu` dihapus bulat-bulat, admin nggak bisa logout dari UI.
- `SideMenu.tsx` sempat ke-delete karena salah baca permintaan, sudah dikembalikan utuh (wajib ada原因: tanpa itu nggak ada jalan ke kategori dari halaman non-home).
- Files: `frontend/components/UserMenu.tsx` (tulis ulang), `frontend/components/SideMenu.tsx` (restore), `frontend/app/layout.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 10/10 route. Live E2E: header screenshot — 1 tombol menu (hamburger) saja; drawer masih berisi 5 series + "Semua Series →" + Masuk.

## 2026-10-04 — Sidebar: section "Series" (padanan Pornstars & Models) + `/series` (ponytail)
- Scraping `pornhub.com/#leftMenu` (Lightpanda, baca markup saja — **tidak** klik atestasi umur) → `.scrollableContentUserMenu` berisi 5 `.menuWrapper`: `videosNavigation`, `pornstarNavigation` (=".Pornstars & Models" + submenu), `categoriesNavigation` (collapsible, default `active`), `communitySubmenu`, promo membership.
- **"Pornstars & Models" dipetakan jadi section "Series"** — bentuknya yang diambil (collapsible `subMenuTrigger` + chevron + tombol "Semua … →"), bukan namanya. **Nol migrasi DB**: series sudah ada dari `groupBySeries()`, jadi plansFirewall kolom `stars` yang sempat diusulkan dibatalkan.
- `components/SideMenu.tsx`: ditulis ulang — `Section` sub-komponen (chevron rotate, `aria-expanded`) untuk 2 seksi collapsible (Series default **buka**, Kategori default buka), `VIDEOS` sebagai blok `videosNavigation`. Series menampilkan 10 terbaru + jumlah Eps.
- Ranking pakai `updated_at` (opsi user), **bukan view count** — app ini nggak punya kolom/endpoint views sama sekali, jadi "Top Trending"/"Hottest" mustahil dijamin jujur. `// ponytail:` naik ke `views` kalau suatu saat ditambahkan.
- `app/series/page.tsx` **baru** — padanan "All Pornstars & Models": direktori semua series multi-episode, grid 4 kolom, urut `updated_at` desc. Route ini sebelumnya nggak ada (`/series/[key]` doang).
- Singleton difilter `movies.length > 1` di sidebar **dan** `/series` — sebelum itu sidebar nampilin "MIDA-472 · 1 Eps", "Chainsawman Movie · 1 Eps" yang bukan series.
- `layout.tsx`: `NAV` nambah `SERIES` (Pornhub punya PORNSTARS di nav atas); hitung `seriesLinks` terurut `updated_at` desc dan kirim ke `SideMenu`.
- Community/Contest Hub/Top Members/Hall Of Fame/membership/FANCENTRO/Shop **dilewati** sesuai instruksi — butuh backend sendiri (user, vote, komentar), bukan tampilan.
- Files: `frontend/components/SideMenu.tsx`, `frontend/app/series/page.tsx` (baru), `frontend/app/layout.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 10/10 route. Live E2E Chrome DevTools MCP: `/` 200, `/series` 200 (5 series, urut benar), `/movies/9` 200; drawer → section Series berisi tepat 5 series (A Bone Fide Killer 14, Yuru Camp 12, My Home Hero 10, Nichijou 26, Log Horizon 59) + "Semua Series →", tanpa singleton.

## 2026-10-04 — Search pindah ke header bar, related rail diversified (ponytail)
- `layout.tsx` jadi `async`: `listMovies()` + `groupBySeries()` di server, katalog dikirim ke header. `.catch(() => [])` di call-site — `get()` lempar saat backend mati, tanpa itu `/login` + `/admin` ikut 500 (DONE: user punya backend hidup, backend mati = degradasi "tanpa kotak search", bukan 500).
- `SearchBox` dipindah dari konten home ke header bar (`rounded-l-sm`/`rounded-r-sm`, `py-1.5`, dropdown `z-50` biar nggak ketutup header `z-30`). `HomeApp` lepas import. Autocomplete dropdown **dipertahankan** — frontend dipindah, bukan diganti form.
- `SideMenu` sekarang menerima prop `genres` dari layout; fetch `listMoviesClient()`-nya dihapus (1 request, bukan 2).
- `RowCard` + prop `meta` opsional: halaman series kini judul card = judul film asli, meta = "Episode 07" (sebelumnya judul card = "Episode 07", judul film nggak kelihatan sama sekali).
- `movies/[id]/page.tsx`: related sekarang buang series sendiri (`seriesKeyForTitle`) **dan** cap 2 item per series — sebelum ini rail 12 episode "A Bone Fide Killer" beruntun,页面看完没有换气的地方.
- `series/[key]/page.tsx`: header season uppercase + garis bawah + count, grid `2/3/4`, gap `gap-x-2.5 gap-y-4` biar sama dengan grid home.
- Files: `frontend/app/{layout.tsx,movies/[id]/page.tsx,series/[key]/page.tsx}`, `frontend/components/{SearchBox,SideMenu,HomeApp,RowCard}.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` 9/9 route. Live E2E via Chrome DevTools MCP: home 200 (grid 4 kolom, sub-nav kategori, drawer hamburger 3 seksi OK — `Escape` + scrim close OK), `/movies/9` 200 (player kiri + rail kananRelatedNx diversity OK). Backend `:3004` 200.
- Catatan operasional: `next start` menyajikan CSS 500 kalau `.next` di-build ulang tanpa restart → **selalu restart `next start` tiap `npm run build`**.

## 2026-10-04 — Sidebar drawer ala Pornhub, grid 4 kolom (ponytail)
- Observasi DOM pornhub.com pakai Lightpanda (baca markup, **tidak** klik atestasi umur): grid home = `.full-row-thumbs.display-grid.col-3-sm.col-4` → **4 kolom desktop, bukan 5**. Sidebar bukan kolom tetap, tapi drawer hamburger `#leftMenu` 4 seksi + header `#headerContainer` (hamburger di kiri logo).
- `components/SideMenu.tsx` (baru): tombol hamburger inline SVG di kiri logo, drawer geser 280px `bg-crust` + scrim + `role=dialog`, kunci scroll body, tutup saat `Escape`/klik scrim/navigasi. Seksi: Jelajahi (Semua Film, Lanjutkan Menonton, Bookmark, Riwayat) / Kategori (genre dari `listMoviesClient()`, maks 24 + "Semua Kategori →") / Akun (Log In). 1 file, tanpa state global.
- `layout.tsx`: mount `<SideMenu />` + `gap-8` → `gap-3` (room buat hamburger).
- Grid 5 kolom → 4 di `HomeApp.tsx` dan `movies/[id]/page.tsx`, sesuai `col-3-sm col-4`.
- Files: `frontend/components/SideMenu.tsx` (baru), `frontend/app/layout.tsx`, `frontend/components/HomeApp.tsx`, `frontend/app/movies/[id]/page.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` sukses 9/9 route. Live E2E BELUM — backend `:3004` masih mati, nunggu `gorun` user.
- Known gap vs pornhub: search masih di konten, belum pindah ke header bar; tidak ada orientation dropdown (Straight/Gay/Sapphic/Trans) — tidak relevan untuk film legal; tidak ada ads/blokAF.

## 2026-10-04 — Restrukturisasi frontend ke gaya Pornhub (ponytail)
- `layout.tsx`: header item (logo `FilmGo` oranye + nav uppercase + UserMenu) di atas `bg-crust` (hitam), konten melebar `max-w-[1600px]` (sebelumnya `max-w-6xl`), footer multi-kolom + baris copyright. Nav/footer dari 1 array `NAV`, tanpa state.
- `HomeApp.tsx`: hero Netflix + `MovieRow` dibuang; ganti sub-nav kategori (bar gelap, chip uppercase scrollable) + 1 grid padat `2/3/4/5` kolom + paginasi bernorum gaya Pornhub (`1 … 4 5 6 … 12`). "Lanjutkan Menonton" dipertahankan sebagai grid (bukan dihapus) → §41.
- `RowCard.tsx`/`SeriesCard.tsx`: kartu Pornhub — `rounded-sm`, badge durasi kanan-bawah `bg-black/85`, judul `line-clamp-2` 0.8rem yang jadi oranye saat hover, baris meta tahun·ukuran. Progress bar 3px.
- `movies/[id]/page.tsx`: halaman tonton 2 kolom — player `flex-1` + rail "Video terkait" 360px (thumb 120×68, daftar vertikal) + grid "Related Videos" 5 kolom bawahnya. Related = `listMoviesClient()` diurutkan skor genre yang sama. Deskripsi jadi kotak bergaris `bg-crust`, tag genre jadi chip yang link ke `/?genre=`.
- `MovieRow.tsx` dihapus (sudah tak ada pemanggil) + aturan `.row-scroll` di `globals.css` ikut dibuang. Tailwind `line-clamp-2` dipakai, bukan CSS clamp custom.
- `globals.css`: palet final Pornhub (`--color-crust: #000`, `mantle: #1b1b1b`, `mauve: #ff6f00`), body polos tanpa radial-gradient.
- Files: `frontend/app/{layout.tsx,globals.css,movies/[id]/page.tsx}`, `frontend/components/{HomeApp,RowCard,SeriesCard}.tsx`, hapus `frontend/components/MovieRow.tsx`.
- Tests: `npx tsc --noEmit` exit 0; `npm run build` sukses 9/9 route. Live E2E BELUM — backend `:3004` dimatikanuser, nunggu `gorun`. Known: backend harus hidup dulu, SSR fetch ke `:3004` kalau mati → 500.

## 2026-10-04 — Palet Pornhub, logo FilmGo (ponytail)
- Ganti palet catppuccin → charcoal Pornhub (#1a1a1a/#232323) + accent oranye `--color-mauve: #ff6f00`; gradient body jadi netral. Token accent dipakai ~80 tempat, jadi cukup ubah 1 token, bukan 80 file.
- Logo tetap "Film" + "Go", cuma `Go` sekarang oranye.
- Files: `frontend/app/globals.css`.
- Tests: `npx tsc --noEmit` exit 0. Layout grid/sidebar BELUM ikut (masih row-scroll Netflix) — menyusul.

## 2026-10-01 — Subtitle varian: Indonesia, Indonesia2, … (ponytail)
- Migrasi `0005`: cabut `UNIQUE(movie_id, language)` via recreate (65 rows pindah utuh, idempoten); daftar di `db.go`.
- Upload: hapus gate 409 → label tabrakan auto-nomor (`Indonesia`→`Indonesia2`→`…3`, existing tak disentuh); file `{movie}_{lang}_{millis}.vtt` + loop anti-tabrakan ms; player nol ubah (sudah multi-track).
- Tes dimutakhirkan (dup → 201 + renumber + path unik); frontend tampilkan label balikan server.
- Files: `backend/migrations/0005*`, `database/{db,subtitles_test}.go`, `api/{admin_media,admin_media_test}.go`, `frontend/app/admin/movies/[id]/page.tsx`.
- Tests: backend suite hijau; live E2E (id×2 → Indonesia/Indonesia2, delete cascade 65 kembali, orphan cuma `1_id.vtt` lama); `:3003`+`:3004` Ready.

## 2026-10-01 — Multi subtitle + port runtime tidak hard-code (ponytail)
- Detail film: picker multi `.vtt/.srt` → rows bahasa+label editable (tebak dari nama: `_ID`/`.en`/`(zh)`/`indonesia`, 9 kasus pass; kode wajib dikenal biar `final cut.srt` tidak ketebak `cut`) + `Upload Semua` sequential, error per row (409 tidak hentikan antrian); UI satuan diganti total.
- `publicBase`: port diambil runtime dari `NEXT_PUBLIC_API_URL` ( `:8090` hard-code mati total, `:3005` stale dimatikan, serve `:3003` baru).
- Files: `frontend/{app/admin/movies/[id]/page.tsx,lib/api.ts}`.
- Tests: 9 kasus tebak pass; build pass; live E2E film coba (srt asli SONE 201 + vtt 201 + dup 409 + delete 200); `:3003`+`:3004` Ready, thumb `:3004`.

## 2026-09-30 — /api/admin/me jawab 200 null saat anonim (ponytail)
- Hilangkan baris merah 401 di console browser: frontend `me()` sudah map non-OK → null, jadi body `null` berperilaku identik tanpa ubah frontend.
- `RequireAdmin` dipisah lookup sesi (`sessionUser`); endpoint admin lain tetap 401/403.
- Files: `backend/internal/api/admin_auth.go`, `backend/internal/api/admin_auth_test.go`.
- Tests: backend suite hijau; live anon /me → `null` 200, anon /api/admin/movies → 401, /api/movies → 200.

## 2026-09-30 — Port pindah: backend 3004, frontend 3003 (ponytail)
- Runtime `publicBase` + default `FRONTEND_URL` + contoh env pindah ke 3004/3003; `.env` user (SERVER_PORT, FRONTEND_URL, API_URL/NEXT_PUBLIC_API_URL) + rebuild frontend manual.
- Files: `frontend/lib/api.ts`, `backend/internal/config/config.go`, `backend/internal/api/cors_test.go`, `.env.example`, `frontend/.env.example`.
- Tests: backend suite + frontend build; live `*:3003`+`*:3004` Ready.

## 2026-09-29 — Bulk metadata massal per series (ponytail)
- Dashboard mode `Pilih`: checkbox per card + dropdown `+ Pilih 1 series` (reuse `groupBySeries`) + `Pilih hasil filter`; panel genre chips/deskripsi/tahun (kosong = pertahankan, karena PATCH overwrite deskripsi) + Terapkan/Publish/Unpublish sequential per film.
- Tanpa backend/endpoint baru (loop `PATCH` + publish yang sudah ada).
- Files: `frontend/app/admin/page.tsx`.
- Tests: build pass; live E2E film coba (create→patch desc/genres/year→delete, 200 semua); `*:3005`+`*:8090` Ready.

## 2026-09-29 — Admin filter huruf + pagination 24 (ponytail)
- Strip `∗ A–Z #` di atas grid (huruf kosong disabled, dihitung dari list); klik huruf filter awalan judul, AND dengan search, reset hal 1.
- Pager `‹ n/m ›` + `n / total film · hal n/m`; murni client-side.
- Files: `frontend/app/admin/page.tsx`.
- Tests: build pass; live `/admin` 200.

## 2026-09-29 — Batch series upload 1-per-1 (ponytail)
- Dashboard `+ Batch Series`: judul series sekali + meta bersama (deskripsi/genre/tahun) + picker multi-file; episode dari nama file (`- 01`, `Eps01`, `S2E05` → `Base [- S2] - NN`); upload sequential XHR per row + preview + error lokal.
- Tanpa endpoint/route/file baru; `createMovie` expose `genres` (backend sudah support).
- Files: `frontend/{lib/admin.ts,app/admin/page.tsx}`.
- Tests: 10 kasus parse pass (`S2E05` fix `(?![0-9])`); build pass; live `*:3005`+`*:8090` Ready.

## 2026-09-29 — Admin card kompak + badge sub (ponytail)
- `subtitle_count` query-time di `ListAll` (1 GROUP BY, tanpa migrasi, `omitempty` → publik tak berubah).
- Dashboard: baris memanjang → grid kompak tanpa gambar (judul, badge status DRAFT/READY, `N sub`, tombol Edit/Retry/Hapus); search tetap.
- Files: `backend/internal/database/movies.go`, `frontend/{lib/api.ts,app/admin/page.tsx}`.
- Tests: backend suite pass; live: admin 152 film (56 bersub), publik 127 tanpa field baru; `*:8090`+`*:3005` Ready.
- Revisi: badge `N sub` + tombol Hapus pindah ke kolom kanan card (kiri: judul/status/Edit); Hapus `mt-auto` → selalu kanan bawah.

## 2026-09-23 — S1 jadi Episode (ponytail)
- `episodeLabel`: S1 `Episode 01`, S2+ `S2 Episode 01` (bukan `S2 E01`).
- Live: `/series/log-horizon` S1 22×Episode + S2 25×S2 Episode + S3 12×S3 Episode.
- Files: `frontend/lib/series.ts`.
- Tests: build pass; curl verified.

## 2026-09-23 — Auto series untuk ` - NN` generik (ponytail)
- `series.ts` fallback `Title - NN` (spasi-dash-spasi, 1-2 digit) → S1 E NN; `A Bone Fide Killer - 01…05` → 1 card `5 Eps` + `/series/a-bone-fide-killer`; `MIDA-964`/tahun tetap singleton.
- Dari sekarang judul ` - xx` apapun auto ke-group (sesuai permintaan), `BD`/`LA`/`S` tetap jalur utama.
- Files: `frontend/lib/series.ts`.
- Tests: 10 kasus pass (Bone Fide 5, Godfather, LA/BD, MIDA/tahun); build pass; live `/series/a-bone-fide-killer` 5 eps, home card baru.

## 2026-09-23 — Binary survive reboot (ponytail)
- `~/.bashrc` `web-go` pindah dari `/tmp/web-go-server` → `./bin/web-go-server`; reuse binary jika sudah ada, `bin/` sudah di `.gitignore`.
- Tambah `web-go-rebuild` untuk rebuild paksa; `web-go-stop` pakai `pkill -f web-go-server`.
- Build: `go build -o ./bin/web-go-server ./backend/cmd/server` (30M).
- Files: `~/.bashrc`, `bin/web-go-server` (ignored).
- Tests: `go build` ok; next `web-go` tanpa compile ulang (cepat).

## 2026-09-20 — Admin search di samping Tambah Film (ponytail)
- Input `Cari film…` di samping `+ Tambah Film`, filter `movies.filter(title includes needle)` client-side, tombol × reset.
- Files: `frontend/app/admin/page.tsx`.
- Tests: build pass; live `*:3005` Ready (admin list ter-filter).

## 2026-09-20 — My Home Hero LA auto masuk series (ponytail)
- `series.ts`: tambah `LA` sejajar `BD` (`\b(?:BD|LA)\b` guard + separator), jadi `My Home Hero LA - 01…10` → 1 card `10 Eps` + `/series/my-home-hero`.
- Upload auto: judul pakai pola `<Series> [S<n>] BD|LA - <ep>` langsung ke-group tanpa migrasi/seting.
- Files: `frontend/lib/series.ts`.
- Tests: python unit 8 kasus pass; build pass; live: hero+grid `/series/my-home-hero` 10 eps, `Under Ninja LA` tetap singleton, `MIDA-964` tetap aman.

## 2026-09-20 — Hero auto masuk series (ponytail)
- Hero cari group di `series`; kalau >1 film → link/judul/badge ke `/series/[key]` + `Lihat Series`, singleton tetap `/movies/[id]`.
- Files: `frontend/components/HomeApp.tsx`.
- Tests: build pass; live: hero `href="/series/log-horizon"` + badge.

## 2026-09-20 — Genre multi + BD tanpa di judul (ponytail)
- `BD` dikecualikan dari judul tampil: series page `S2 E01` / `Episode 01` tanpa `BD -`.
- Genre: kolom `genres` (migrasi 0004, 6 nilai: comedy/drama/romance/fantasy/crime/horror, max 5) + validasi.
- Admin: chips klik di metadata; Home: chips filter `All` + genre; Detail: badge; Search popup ikut match genre.
- Files: `migrations/0004_genres.sql`, `database/{db,movies}.go`, `api/admin_movies.go`, `frontend/{lib/api,lib/series,app/movies/[id]/page,app/admin/movies/[id]/page,components/HomeApp,components/SearchBox,app/series/[key]/page,components/RowCard}.tsx`.
- Tests: backend suite pass; `srt` test dimutakhirkan; live: filter & badge ok, 93 film siap edit.

## 2026-09-20 — Search popup modern (ponytail)
- Ketik → dropdown max 6 saran (series + episode) dengan thumb + judul bold, keyboard ↑↓/Enter, klik/Enter navigasi.
- Grid home tetap (tidak ketimpa hasil search) — popup overlay.
- Files: `frontend/components/SearchBox.tsx`, `frontend/components/HomeApp.tsx` (hapus grid hasil-search, pager tetap).
- Tests: build pass; live: ketik muncul popup, grid tetap.

## 2026-09-20 — Hapus play tengah + .srt auto jadi .vtt
- `Player.tsx` hapus overlay play tengah (kembali native controls saja).
- `handleUploadSubtitle` terima `.srt` + `.vtt`; `.srt` dikonversi ke WebVTT (`srtToVTT`) on-the-fly.
- Frontend `accept=".vtt,.srt"` + label `Subtitle (.vtt/.srt)`.
- Files: `frontend/components/Player.tsx`, `backend/internal/api/admin_media.go`, `frontend/app/admin/movies/[id]/page.tsx`.
- Tests: build pass; live `*:3005` + `*:8090` Ready.

## 2026-09-20 — Grouping series (home card per series + halaman per season)
- Parser `Log Horizon S2 BD - 01` → series/season/episode; `groupBySeries` tanpa migrasi DB.
- Home grid jadi card series (`X Eps` badge, cover episode pertama); pagination hitung series.
- Halaman `/series/[key]` daftar episode per season.
- Files: `frontend/lib/series.ts`, `frontend/components/{HomeApp,SeriesCard}.tsx`, `frontend/app/series/[key]/page.tsx`.
- Tests: build pass; live: `/` card log-horizon/nichijou + badge, `/series/log-horizon` season split ok (86/92 ter-grouping, 6 singleton tetap tampil).

## 2026-09-20 — Admin metadata judul auto-select saat fokus
- `onFocus` `select()` di input judul — klik langsung terblok, ketik ganti.
- Files: `frontend/app/admin/movies/[id]/page.tsx`.
- Tests: build pass; live `*:3005` Ready; backend/upload tak tersentuh.

## 2026-09-20 — Player tombol play tengah (ala YouTube)
- Overlay lingkaran mauve di tengah video saat pause/awal/selesai; klik toggle play/pause, kontrol bawah tetap native.
- Files: `frontend/components/Player.tsx`.
- Tests: build pass; live `*:3005` Ready.

## 2026-09-20 — Subtitle auto-on + tetap 10% (ponytail)
- `Player.tsx` track pertama `default`, posisi `10%` sudah dari sebelumnya — hard refresh lagi.
- Files: `frontend/components/Player.tsx` (`default={i===0}`).
- Tests: build pass; live `*:3005` Ready.

## 2026-09-20 — Home grid 12 → 20 per halaman
- `PAGE_SIZE 12 → 20` di `HomeApp.tsx`.
- Files: `frontend/components/HomeApp.tsx`.
- Tests: build pass; live `*:3005` Ready.

## 2026-09-20 — Dashboard admin: Pengguna di atas Tambah Film
- Pindahkan seksi Pengguna (form + list + role toggle) ke atas tombol + Tambah Film + list film.
- Files: `frontend/app/admin/page.tsx`.
- Tests: build pass; live `*:3005` Ready.

## 2026-09-20 — Fix login mental + favicon png
- `publicBase` jadi runtime (`window.location.hostname:8090`) — laptop dan HP masing-masing hit host yang sama, cookie Lax kekirim, tidak mental lagi.
- `favicon.png` (1254x1254) → `app/favicon.ico` (16/32/48) + `app/icon.png` via ImageMagick.
- Files: `frontend/lib/api.ts`, `frontend/app/favicon.ico`, `frontend/app/icon.png`.
- Tests: build pass; live: 127.0.0.1 + 192.168.1.10 ready.

## 2026-09-20 — CORS allow both origins + usbell + favicon
- `FRONTEND_URL` include `192.168.1.10:3005` + `127.0.0.1:3005` (laptop+HP), restart backend.
- Akun `usbell` (admin) dibuat.
- `app/favicon.ico` 16x16 valid (Turbopack tidak error, console bersih).
- Files: `.env`, `frontend/app/favicon.ico`, DB users.
- Tests: build pass; live: preflight 204 kedua origin, usbell 200, me 200, favicon 200.


## 2026-09-20 — HP via LAN + sub 10%
- `SERVER_HOST=0.0.0.0`, `FRONTEND_URL` tambah `192.168.1.10:3005`, `NEXT_PUBLIC_API_URL` → `192.168.1.10:8090` (rebuild, CORS allow).
- Subtitle `bottom: 15% → 10%`.
- Files: `.env`, `frontend/.env.local`, `frontend/app/globals.css`.
- Tests: build pass; live: 127.0.0.1 + 192.168.1.10 home 200, api via lan 200.
## 2026-09-19 — Detail fix: anon fallback ke :8090 (ponytail)
- `app/movies/[id]` (client) salah pakai `serverBase` (:8080) → 6 film READY jadi 404. Tambah `getMovieClient`/`listSubtitlesClient` pakai `publicBase`.
- Files: `frontend/lib/api.ts`, `frontend/app/movies/[id]/page.tsx`.
- Tests: `npm run build` pass.

## 2026-09-19 — Playback publik untuk READY (admin juga lihat DRAFT)
- Stream/subtitle publik untuk `READY`; sesi admin fallback ke status lain via `resolveMovie`/`isAdminSession`.
- Detail page selalu tampilkan player/download (DRAFT 404 untuk anon, 200 untuk admin).
- Files: `backend/internal/api/{admin_auth,stream,movies,subtitles}.go`, `frontend/app/movies/[id]/page.tsx`, `PRD.md`.
- Tests: backend suite pass; live: anon READY 200+stream 206, DRAFT 404, admin DRAFT 200.

## 2026-09-19 — Thumb auto 1x ke Telegram + home tanpa preload video
- Thumb generate 1x (ffmpeg 640px) lalu upload ke Telegram via `storage.Storage`; DB `thumb_telegram_*` (migrasi 0003) + serve `GET /api/thumbs` dari Telegram dengan fallback disk lama.
- Home: hapus `<video preload="metadata">` di card & hero — cuma `<img thumb/poster>` jadi buka home nol byte video sampai Play diklik.
- Files: `migrations/0003_thumb_telegram.sql`, `database/{db,movies}.go`, `api/{admin_media,admin_movies}.go`, `frontend/{HomeApp,RowCard}.tsx`.
- Tests: backend suite pass; live: home 0 stream, thumb dari disk fallback OK, migrasi idempotent.

## 2026-09-19 — Subtitle style: tanpa background, kecil, naik
- `video::cue` transparan, 0.88em, text-shadow tanpa kotak; track display diangkat.
- Files: `frontend/app/globals.css`.
- Tests: build pass.

## 2026-09-19 — Subtitle .vtt terkirim dengan cookie sesi
- `<track>` fetch tanpa cookie → 401 diam-diam; tambah `crossOrigin="use-credentials"` (via spread, tak ada di tipe React).
- Files: `frontend/components/Player.tsx`.
- Tests: `tsc` + build pass; atribut terverifikasi di chunk production. Frontend restart saja, backend/upload tak tersentuh.

## 2026-09-19 — Playback + subtitle admin-only
- Stream bytes dan subtitle 401 anonim / 403 user; katalog/detail/artwork tetap publik.
- Detail page: non-admin lihat info + ajakan login ganti player; tombol Download khusus admin.
- Files: `api/admin_auth.go` (gate), tes stream/subtitle/draft dimutakhirkan, `movies/[id]/page.tsx`, PRD §28.5a.
- Tests: full suite pass; live diverifikasi via curl.

## 2026-09-19 — Search di atas hero + A-Z di kanan SEMUA FILM
- Form cari full-width (input + tombol Cari) pindah ke atas hero; tombol A-Z pindah ke kanan heading SEMUA FILM.
- Files: `frontend/components/HomeApp.tsx`.
- Tests: `tsc` + build pass; urutan terverifikasi di HTML.

## 2026-09-19 — Auto-extract embedded subtitles + home grid/pagination
- Pipeline ekstrak track subtitle teks tertanam → `.vtt` per bahasa (best-effort, manual menang, tanpa migrasi).
- Home: grid vertikal responsif + pagination 12/halaman; "Lanjutkan" tetap bar horizontal.
- Files: `api/admin_media*.go`, `admin_movies.go` (hook), `HomeApp`, `RowCard` (w-full).
- Tests: `TestExtractSubtitles` (mkv sintetis 2 track) + full suite pass; live: grid + 0 card error.

## 2026-09-19 — Home admin tampilkan semua film + bisa play
- Helper `resolveMovie`/`isAdminSession`: sesi admin melihat dan memutar status apa pun (DRAFT ikut tayang di home + player); anonim tetap 404.
- Katalog `/api/movies` kembalikan semua status untuk admin (READY saja untuk publik).
- Frontend: home muat list admin bila sesi admin; badge status non-READY di card + detail; detail jadi client component (fetch admin vs publik).
- PRD §13 diamendemen.
- Files: `api/{router,stream,movies,subtitles,admin_media}.go`, `admin_draft_test.go`, `HomeApp`, `RowCard`, `movies/[id]/page`.
- Tests: `TestAdminSeesDrafts` + full suite pass; live: anon 404 vs admin 200 + stream DRAFT 23MB.

## 2026-09-19 — Public catalog restored (gate reverted)
- Katalog/detail/stream/subtitle/poster/thumb publik lagi untuk READY; akun hanya untuk panel admin.
- Revert: route preview tetap dihapus, fixture tes kembali tanpa sesi, ekspektasi 404 DRAFT.
- PRD §13 dikembalikan.
- Files: `api/{admin_auth,stream,subtitles,movies,admin_media}.go`, tes api, `frontend/app/{page,movies/[id],bookmarks,history}`.
- Tests: full suite pass; live anon 200 + stream 206.

## 2026-09-19 — Admin-only catalog + preview route removed
- Home/detail/stream/subtitle/poster/thumb 401 anonim, 403 role user, 200 admin (termasuk stream DRAFT).
- Hapus route preview + `previewUrl` + player preview admin (diganti gate seragam).
- Frontend: home/detail/bookmark/history tampil ajakan login bila bukan admin.
- PRD §13 + §28.5a diamendemen.
- Files: `api/{admin_auth,stream,subtitles,movies,admin_media}.go`, `gate_test.go`, fixture tes, `frontend/app/{page,movies/[id],bookmarks,history}`, `SiteNav` hapus.
- Tests: full suite pass (fixture + ekspektasi dimutakhirkan); live: 5×401 anon, admin list/stream 206, nol abort non-batal.

## 2026-09-18 — Upload progress + log panel + admin preview
- Upload video via XHR dengan bar "Mengirim ke server %" (fetch tak punya progress event).
- Panel "Log proses" terminal-style: baris berwaktu per transisi part, baris live (byte + MB/s + elapsed), error merah, auto-scroll.
- Preview khusus admin: `GET /api/admin/movies/{id}/preview` (RequireAdmin, status apa pun) + player di halaman admin. Publik tetap 404 untuk non-READY.
- Files: `backend/internal/api/{stream,admin_auth}.go`, `preview_test.go`, `frontend/lib/{admin,api}.ts`, `frontend/app/admin/movies/[id]/page.tsx`.
- Tests: `TestAdminPreview` + full suite pass; `tsc` + build pass; live: preview 206, no-auth 401, publik 206, nol abort.

## 2026-09-18 — One-click movie create + admin file-row layout
- Dashboard: single "+ Tambah Film" button (no title-first); backend fills "Film Baru" + unique slug.
- Detail page: labeled file-picker buttons + filename display + full-width Upload for video/poster/backdrop/subtitle; inputs reset after upload.
- Files: `backend/internal/api/admin_movies.go`, `frontend/lib/admin.ts`, `frontend/app/admin/page.tsx`, `frontend/app/admin/movies/[id]/page.tsx`.
- Tests: backend suite pass (updated TestAdminCRUD); `tsc` + `next build` pass; live blank-create→delete verified.

## 2026-09-18 — Popup menu simplified to buttons
- UserMenu is now buttons only (account, Dasbor Admin, Bookmark, History, Keluar); inline lists removed, outside-click/Escape closes.
- Files: `frontend/components/UserMenu.tsx`.
- Tests: `tsc` + `next build` pass; served HTML verified.

## 2026-09-18 — Ponytail safe-cuts + streaming refetch fix
- Deleted dead code: `Retryable`, server `progress.go`, `GetSubtitle`, `adminMovieID`/`movieIDParam`→`pathID`, `missingPartError`→sentinel, `MovieModal`, `UserMenu`→restored later, `WatchedButton`/`BookmarkButton`→merged→reverted (RSC violation, see below).
- Merged: subtitle/media helpers, admin user types, fetch/catalog mappers, publish pair.
- Reverted 2 audit claims after verification: `SubtitleExists` (protects existing file), `ToggleChip` (function props illegal in server→client boundary, caused detail 500).
- Fixed: Telegram file-reference expiry now refetches across exact + fallback paths; process-local ref cache.
- Files: `backend/internal/{api,telegram,database}/*`, `frontend/{components,lib,app}/**`, `PRD.md`, `AGENTS.md`, `.gitignore`, `frontend/package.json`.
- Tests: `go test ./backend/...` pass; `tsc` + `next build` pass; stream ranges 206 exact, zero aborts.

## 2026-09-18 — 6 pages + public accounts (PRD §28.5a)
- Register/login with roles (`user`/`admin`), admin user CRUD, `RequireAdmin` 403 gate.
- Pages: `/`, `/movies/[id]`, `/bookmarks`, `/history`, `/admin`, `/login`. Modal removed.
- Files: `backend/internal/api/admin_users.go`, `database/users.go`, `frontend/app/{login,bookmarks,history}`, detail/admin pages, nav.
- Tests: new `admin_users_test.go` + full suite pass; curl-verified register→403→promote→delete.

## 2026-09-18 — Streaming + upload hardening
- 1024-align + 512K-block fallback for Telegram `upload.getFile` (fixes spinning video).
- Auto ffmpeg thumbnail on finalize + `/api/thumbs/{id}`; TMPDIR→repo `tmp/` (tmpfs too small).
- FK pragma via DSN (orphan parts), multipart spool cleanup, CORS middleware, upload no-file guards.
- Files: `telegram/storage.go`, `api/{admin_media,admin_movies,admin_auth,cors}.go`, `database/db.go`, frontend cards/hero/menu.
- Tests: backend suite + probes (byte-identical fallback); curl-verified ranges.

## 2026-09-18 — Ops aliases
- `web-go` / `web-go-stop` / `web-go-build` in `~/.bashrc`; ports 8090/3005; fixed `kill`-alias footgun with `command kill`.

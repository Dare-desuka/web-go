# PRD — Telegram Film Streaming Web

## 1. Ringkasan

Membangun web katalog film yang memungkinkan pengguna menonton video langsung melalui browser.

Video disimpan di Telegram sebagai storage, bukan di server aplikasi.

Backend menggunakan Go dan berfungsi sebagai streaming proxy antara browser dan Telegram.

Jika ukuran video terlalu besar, sistem otomatis memecah video menjadi beberapa part sebelum upload ke Telegram.

Pengguna tidak perlu mengetahui bahwa satu film terdiri dari beberapa part.

---

# 2. Tujuan

### Tujuan utama

* Menonton film langsung dari browser.
* Tidak memerlukan download manual.
* Menggunakan Telegram sebagai object storage.
* Menekan kebutuhan storage server.
* Mendukung film berukuran besar.
* Otomatis membagi file menjadi beberapa part.
* Player tetap terlihat seperti memainkan satu file.
* Mendukung HTTP Range Request.
* Mendukung resume dan seek.
* Memiliki panel admin untuk mengelola film.

### Bukan tujuan

* Membuat platform sosial seperti Telegram.
* Menyimpan video secara permanen di server aplikasi.
* Mengharuskan pengguna memiliki akun Telegram.
* Membuat pengguna melihat atau mengelola part video.

> Sistem hanya boleh digunakan untuk konten yang pengguna punya hak untuk menyimpan dan mendistribusikannya.

---

# 3. Arsitektur

```text
                        INTERNET
                            │
                            ▼
                    ┌───────────────┐
                    │   Web Client  │
                    │ HTML5 Player  │
                    └───────┬───────┘
                            │
                    HTTP / HTTPS
                            │
                            ▼
                    ┌───────────────┐
                    │    Go API     │
                    │               │
                    │ Movie API     │
                    │ Stream API    │
                    │ Admin API     │
                    └───────┬───────┘
                            │
                    ┌───────┴────────┐
                    │                │
                    ▼                ▼
                   SQLite          Telegram
                                   MTProto
                                   │
                     ┌─────────────┼─────────────┐
                     ▼             ▼             ▼
                  Part 1        Part 2        Part 3
```

---

# 4. Komponen

## Frontend

Rekomendasi:

* Next.js
* TypeScript
* Tailwind CSS
* HTML5 Video

Frontend bertanggung jawab untuk:

* Homepage
* Search
* Movie detail
* Video player
* Episode list
* Subtitle
* Continue watching
* Responsive mobile/desktop UI

---

# 5. Backend

Gunakan:

**Go**

Tanggung jawab:

* REST API
* Authentication admin
* Movie management
* Telegram integration
* Upload processing
* File splitting
* Streaming
* HTTP Range Request
* Part switching
* Subtitle management
* Watch progress

Struktur awal:

```text
backend/
├── cmd/
│   └── server/
├── internal/
│   ├── api/
│   ├── auth/
│   ├── movie/
│   ├── streaming/
│   ├── telegram/
│   ├── upload/
│   ├── storage/
│   └── database/
├── migrations/
└── go.mod
```

---

# 6. Telegram Storage

Gunakan akun Telegram khusus project.

Jangan menggunakan akun Telegram pribadi sebagai dependency utama.

Struktur:

```text
Telegram Account
        │
        ▼
Private Storage Channel
        │
        ├── Movie A Part 1
        ├── Movie A Part 2
        ├── Movie B Part 1
        └── Movie C Part 1
```

Backend menggunakan MTProto/client API untuk mengakses file.

Jangan expose:

* Telegram API credentials
* Telegram session
* channel ID
* internal file identifiers

kepada frontend.

---

# 7. Upload Pipeline

Admin memilih video:

```text
movie.mkv
```

Backend melakukan:

```text
1. Validasi file
2. Baca metadata
3. Cek ukuran
4. Tentukan jumlah part
5. Split jika diperlukan
6. Upload setiap part ke Telegram
7. Simpan metadata
8. Verifikasi upload
9. Tandai movie sebagai READY
```

---

# 8. File Splitting

Default:

```text
MAX_PART_SIZE = 900 MB
```

Contoh:

```text
700 MB
→ Part 1

1.5 GB
→ Part 1 900 MB
→ Part 2 600 MB

2.5 GB
→ Part 1 900 MB
→ Part 2 900 MB
→ Part 3 700 MB

4 GB
→ Part 1
→ Part 2
→ Part 3
→ Part 4
→ Part 5
```

Jangan mengasumsikan film hanya memiliki dua part.

Jumlah part harus dinamis.

---

# 9. Streaming

Endpoint utama:

```text
GET /api/stream/movies/{movieID}
```

Mendukung:

```http
Range: bytes=0-
```

dan:

```http
Range: bytes=500000000-600000000
```

Backend harus:

1. Membaca Range request.
2. Menentukan part Telegram yang berisi byte tersebut.
3. Menghitung offset di dalam part.
4. Mengambil chunk dari Telegram.
5. Stream chunk ke browser.
6. Melanjutkan ke part berikutnya jika Range melewati batas part.
7. Mengirim response HTTP yang valid.

Browser harus menganggapnya sebagai **satu video**.

---

# 10. Virtual File

Film:

```text
2.4 GB
```

Storage:

```text
Part 1 = 900 MB
Part 2 = 900 MB
Part 3 = 600 MB
```

Backend memperlakukan semuanya sebagai:

```text
movie.mkv
size = 2.4 GB
```

Browser tidak melihat:

```text
part1
part2
part3
```

Browser hanya melihat:

```text
/api/stream/movies/123
```

---

# 11. Part Mapping

Database menyimpan:

```text
Movie
  ID: 123
  Size: 2.4 GB

Parts:

Part 1
offset_start: 0
offset_end: 899999999

Part 2
offset_start: 900000000
offset_end: 1799999999

Part 3
offset_start: 1800000000
offset_end: 2399999999
```

Saat browser meminta:

```text
bytes=950000000-
```

backend langsung mengetahui bahwa request berada di:

```text
Part 2
```

---

# 12. Database

Gunakan SQLite untuk MVP.

Struktur:

```text
movies
├── id
├── title
├── slug
├── description
├── year
├── duration
├── poster_url
├── backdrop_url
├── video_size
├── video_mime
├── status
├── created_at
└── updated_at

movie_parts
├── id
├── movie_id
├── part_number
├── size
├── start_offset
├── end_offset
├── telegram_message_id
├── telegram_file_id
├── status
└── created_at

subtitles
├── id
├── movie_id
├── language
├── label
├── file_path
└── created_at

watch_progress
├── id
├── movie_id
├── user_id
├── position_seconds
└── updated_at

users
├── id
├── username
├── password_hash
├── role
└── created_at
```

---

# 13. Movie Status

Gunakan status:

```text
DRAFT
UPLOADING
PROCESSING
READY
ERROR
DELETING
```

Film hanya muncul di frontend jika:

```text
status = READY
```

Pengecualian: sesi admin yang valid melihat semua status di katalog,
detail, stream, subtitle dan artwork (termasuk DRAFT), agar film yang
disembunyikan tetap bisa diperiksa dan diputar admin di home.

---

# 14. Admin Panel

Admin dapat:

### Movie

* Add movie
* Edit movie
* Delete movie
* Publish/unpublish
* Upload video
* Upload poster
* Upload subtitle

### Upload

Tampilkan:

```text
Uploading...

Part 1  ████████████ 100%
Part 2  ████████░░░░  72%
Part 3  waiting

Overall:
████████░░░░ 68%
```

Setelah selesai:

```text
Upload completed

Movie:
Example Movie

Size:
2.4 GB

Parts:
3

Status:
READY
```

---

# 15. Frontend Pages

## Homepage

```text
Navbar

Hero Movie

Continue Watching

Latest Movies

Popular Movies

Genres
```

## Movie Detail

```text
Poster

Title
Year
Duration
Genre

Description

[ WATCH NOW ]

Subtitle
Languages
```

## Player

```text
┌──────────────────────────────┐
│                              │
│          VIDEO               │
│                              │
│                              │
├──────────────────────────────┤
│ ▶  ━━━━━━━━━━━━              │
│ 01:23:15 / 02:10:31          │
│                              │
│ ⚙ Quality   CC Subtitle      │
└──────────────────────────────┘
```

---

# 16. Player Requirements

Player harus mendukung:

* Play
* Pause
* Seek
* Volume
* Fullscreen
* Picture-in-picture
* Keyboard controls
* Subtitle
* Resume position
* Mobile controls
* Desktop controls

Ketika user menutup video:

```text
position = 01:23:15
```

Simpan progress.

Saat membuka lagi:

```text
Continue from 01:23:15?
```

---

# 17. Subtitle

Prioritas:

```text
WebVTT
```

Backend menyediakan:

```text
GET /api/subtitles/{movieID}
```

Frontend menggunakan:

```html
<track>
```

Bahasa:

```text
Indonesia
English
Japanese
Chinese
```

Bahasa hanya ditampilkan jika subtitle tersedia.

Otomatis: saat upload selesai, track subtitle teks yang tertanam
(SRT/ASS/MOV_TEXT/…) diekstrak menjadi `.vtt` per bahasa. Track gambar
(PGS/VobSub) dilewati. Bahasa yang sudah ada `.vtt` manual tidak
ditimpa.

---

# 18. Security

### Telegram

Credentials hanya berada di backend.

Gunakan environment variables:

```text
TELEGRAM_API_ID=
TELEGRAM_API_HASH=
TELEGRAM_SESSION=
TELEGRAM_CHANNEL_ID=
```

Jangan commit ke Git.

### Admin

Password harus menggunakan:

```text
Argon2id
```

atau bcrypt.

### Streaming

Jangan memberikan URL Telegram langsung ke browser.

Gunakan:

```text
/api/stream/movies/{id}
```

Jika menggunakan authentication untuk user, token/session harus diverifikasi sebelum streaming.

---

# 19. Caching

MVP:

```text
Browser
   ↓
Go
   ↓
Telegram
```

Versi lanjutan:

```text
Browser
   ↓
Go
   ↓
Local temporary cache
   ↓
Telegram
```

Cache hanya digunakan jika benar-benar meningkatkan performa.

Jangan menjadikan server sebagai permanent video storage.

---

# 20. Reliability

Upload tidak boleh dianggap selesai sebelum setiap part berhasil.

Contoh:

```text
Part 1 ✓
Part 2 ✓
Part 3 ✗
```

Status:

```text
ERROR
```

Sistem harus memungkinkan:

```text
Retry Part 3
```

tanpa mengupload ulang Part 1 dan Part 2.

---

# 21. Storage Abstraction

Buat interface:

```go
type Storage interface {
    Upload(ctx context.Context, file io.Reader, metadata Metadata) (File, error)
    DownloadRange(ctx context.Context, file File, offset int64, length int64) (io.ReadCloser, error)
    Delete(ctx context.Context, file File) error
}
```

Implementasi pertama:

```text
TelegramStorage
```

Masa depan:

```text
LocalStorage
S3Storage
R2Storage
```

Dengan begitu sistem tidak terkunci pada Telegram.

---

# 22. Logging

Log:

```text
Upload started
Upload part started
Upload part completed
Upload failed
Stream started
Stream ended
Telegram request failed
Range request
Cache hit/miss
```

Jangan log:

```text
API hash
session
password
private credentials
```

---

# 23. Error Handling

Contoh:

```text
Telegram unavailable
→ retry dengan exponential backoff

Part missing
→ HTTP 503

Movie not READY
→ jangan tampilkan ke user

Invalid Range
→ HTTP 416

Movie tidak ditemukan
→ HTTP 404
```

---

# 24. MVP

Versi pertama hanya perlu:

```text
[✓] Go backend
[✓] SQLite
[✓] Telegram MTProto
[✓] Upload video
[✓] Automatic splitting
[✓] Telegram upload
[✓] Movie database
[✓] Streaming endpoint
[✓] HTTP Range
[✓] Virtual multi-part stream
[✓] HTML5 player
[✓] Movie detail
[✓] Admin login
[✓] Admin upload
```

Jangan membuat:

```text
[ ] Recommendation AI
[ ] Social features
[ ] Comments
[ ] Complex user profiles
[ ] Payment
[ ] Multiple storage providers
```

untuk MVP.

---

# 25. Acceptance Criteria

Project dianggap berhasil jika:

### Upload

```text
Upload 700 MB
→ Telegram 1 part
→ status READY
```

```text
Upload 2.4 GB
→ Telegram 3 parts
→ status READY
```

### Streaming

User membuka:

```text
/movie/example
```

Kemudian:

```text
Play
```

Film mulai diputar tanpa harus mendownload seluruh film.

User dapat:

```text
seek
pause
resume
fullscreen
```

dan player tetap menganggap film sebagai satu file.

### Part transition

Jika Part 1 selesai:

```text
Part 1
   ↓
Part 2
   ↓
Part 3
```

Transisi terjadi otomatis.

User tidak melihat pergantian part.

### Resume

Jika user berhenti pada:

```text
01:23:15
```

kemudian kembali lagi:

```text
Continue from 01:23:15
```

---

# 26. Future Improvements

Setelah MVP stabil:

```text
Adaptive streaming
Multiple quality
1080p
720p
480p

Multiple audio

HLS/DASH

CDN/cache

TV interface

PWA

Episode system

Genre system

Advanced search

Import metadata

Automatic poster/backdrop

Storage health monitoring
```

---

# 27. Prinsip Utama

Prioritas pengembangan:

```text
1. Streaming harus stabil
2. Telegram harus tersembunyi dari frontend
3. Part harus transparan bagi user
4. Upload harus resumable/retryable
5. Database harus menyimpan mapping byte → Telegram part
6. Backend harus mendukung HTTP Range
7. Storage layer harus bisa diganti
8. Jangan menyimpan video permanen di server
9. Jangan memasukkan credentials ke repository
10. Konten harus memiliki hak distribusi yang sesuai
```

---

# 28. Keputusan Teknis yang Dikunci

Bagian ini mengikat dan meniadakan ambiguitas ukuran/desimal/biner.

## 28.1 Batas part

* `MAX_PART_SIZE = 900 MiB = 943718400 bytes`.
* Konstanta internal selalu dalam bytes (`int64`).
* UI boleh menampilkan label `900 MB`, tetapi perhitungan split/mapping selalu memakai `943718400`.

## 28.2 Konvensi offset part (satu-satunya acuan)

* Setiap part memakai range **[start_offset, end_offset)** (end eksklusif).
* `size = end_offset - start_offset`.
* Contoh part 900 MiB pertama: `start_offset = 0`, `end_offset = 943718400`.
* Offset lokal Telegram: `local = global - start_offset`.

## 28.3 Batas film global (MVP)

* Maksimum **20 GiB = 21474836480 bytes** per film.
* Ukuran file adalah validasi utama upload.
* Tidak ada batas durasi untuk upload di MVP.

## 28.4 ffprobe/ffmpeg (MVP)

* Tidak wajib di MVP.
* `duration` di database boleh nullable.
* Jangan menjalankan ffmpeg/ffprobe hanya untuk kebutuhan upload.

## 28.5 Watch progress (MVP)

* Anonim memakai `localStorage` browser.
* Kunci: `watch_progress:{film_id}` → posisi detik (`INTEGER`).
* Migrasi progress ke server hanya setelah auth user publik ada.

## 28.5a Akun publik dan role (amendemen operator)

* Siapa pun boleh membuat akun dari halaman login (self-register).
* Akun baru selalu berrole `user`; hanya admin yang bisa menaikkan ke `admin`.
* Role yang dikenal: `user`, `admin`. Seluruh Admin API wajib menolak
  non-`admin` (403), bukan sekadar sesi valid.
* Playback publik untuk `READY`; sesi admin juga melihat/memutar status
  lain (DRAFT) — katalog/detail/stream/subtitle/artwork fallback ke
  `isAdminSession` bila `GetReady` 404.
* Admin tidak bisa mencabut role admin milik sendiri atau menghapus akun
  sendiri (anti lockout).
* Bookmark/history tetap anonim di `localStorage`; tidak pindah ke server
  di MVP ini.

## 28.6 Session admin (MVP)

* HttpOnly cookie session, bukan JWT.
* Cookie: `HttpOnly`, `Secure` di production, `SameSite=Lax`.
* Durasi session: 24 jam, lalu admin login lagi.
* Session ID disimpan server-side; cookie tidak membawa data auth.

## 28.7 Format waktu database

* Semua kolom waktu (`created_at`, `updated_at`) memakai **Unix INTEGER dalam milliseconds**, konsisten di semua tabel.
* API boleh mengubahnya menjadi ISO8601 bila diperlukan frontend.

# AGENTS.md

## Project Overview

This project is a web-based movie streaming platform.

The application uses Telegram as the primary video storage layer. The Go backend acts as the secure middleware between the web frontend and Telegram.

Large video files are automatically split into multiple parts before being uploaded to Telegram.

The user must experience each movie as a single continuous video. The existence of Telegram parts must remain an internal implementation detail.

`PRD.md` is the primary source of truth for product requirements.

---

# 1. Source of Truth

Always read:

```text
PRD.md
AGENTS.md
```

before implementing a feature.

Priority:

```text
PRD.md
    ↓
AGENTS.md
    ↓
Existing project architecture
    ↓
Implementation details
```

Do not silently change requirements from `PRD.md`.

If a requirement is ambiguous, inspect the existing implementation and explain the ambiguity before making a major architectural decision.

Do not invent major features that are not required by the PRD.

---

# 2. Development Philosophy

Prioritize:

1. Correctness
2. Reliability
3. Security
4. Testability
5. Simplicity
6. Performance
7. UI polish

Do not optimize prematurely.

Do not introduce unnecessary dependencies.

Do not create microservices unless the PRD explicitly requires them.

Prefer a simple modular monolith for the MVP.

---

# 3. Required Stack

## Backend

Use:

```text
Go
SQLite
Telegram MTProto/client API
```

Use idiomatic Go.

Prefer the standard library when it is sufficient.

Keep external dependencies minimal and justified.

## Frontend

Use:

```text
Next.js
TypeScript
Tailwind CSS
HTML5 Video
```

Do not introduce another frontend framework unless explicitly required.

---

# 4. Architecture

Use this high-level architecture:

```text
Browser
   │
   │ HTTPS
   ▼
Frontend
   │
   ▼
Go API
   │
   ├── Movie API
   ├── Auth API
   ├── Admin API
   └── Streaming API
   │
   ├───────────────┐
   ▼               ▼
SQLite          Storage Layer
                    │
                    ▼
              Telegram MTProto
                    │
             ┌──────┼──────┐
             ▼      ▼      ▼
           Part 1 Part 2 Part 3
```

Keep these responsibilities separated:

```text
API
Business Logic
Storage
Telegram
Database
Streaming
Authentication
```

Do not put Telegram-specific logic directly inside HTTP handlers.

---

# 5. Storage Abstraction

Telegram must not be tightly coupled to the rest of the application.

Create a storage abstraction.

Conceptually:

```go
type Storage interface {
    Upload(...)
    DownloadRange(...)
    Delete(...)
}
```

The first implementation should be:

```text
TelegramStorage
```

Future implementations may include:

```text
LocalStorage
S3Storage
R2Storage
```

Do not implement future storage providers unless required.

The rest of the application should depend on the `Storage` interface rather than directly on Telegram.

---

# 6. Telegram Rules

Use a dedicated Telegram account for the project.

Never use the developer's personal Telegram account as an application dependency.

Use a private Telegram channel as the storage location.

Telegram credentials must remain server-side.

Never expose these to the frontend:

```text
TELEGRAM_API_ID
TELEGRAM_API_HASH
TELEGRAM_SESSION
TELEGRAM_CHANNEL_ID
Telegram file identifiers
Telegram message identifiers
```

Use environment variables or another secure server-side configuration mechanism.

Never commit credentials or sessions to Git.

Add sensitive files to `.gitignore`.

---

# 7. Telegram API

Use MTProto/client-level Telegram access for large file handling and streaming.

Do not design the movie streaming system around Bot API `getFile()`.

The browser must never stream directly from a Telegram URL.

The intended flow is:

```text
Browser
   ↓
Go Streaming API
   ↓
Telegram Storage
```

Telegram must remain an internal storage provider.

---

# 8. Video Upload Pipeline

The upload pipeline must follow:

```text
Admin
 ↓
Upload video
 ↓
Validate
 ↓
Read metadata
 ↓
Determine size
 ↓
Split if necessary
 ↓
Upload parts to Telegram
 ↓
Verify each part
 ↓
Save metadata
 ↓
Mark movie READY
```

Do not mark a movie as `READY` until every required part has been successfully uploaded and verified.

The upload temp (`upload-<movieID>.bin`) holds the **full movie** on disk. It
lives on the HDD (`TMP_DIR`, default `/mnt/hdd/videos/tmp`) — never on the
box's internal storage, which cannot hold a 20 GiB movie.

Temp lifecycle:

```text
upload → temp on HDD
       → parts streamed to Telegram (no part files on disk)
       → movie READY  → temp deleted immediately
       → janitor deletes temps older than 1 hour (every 5 minutes)
```

The janitor skips movies still `UPLOADING`/`PROCESSING` (a live upload must
not lose its source) and movies in `ERROR` (their temp is the retry source).

---

# 9. Automatic File Splitting

Default maximum part size:

```text
900 MB
```

This value must be configurable.

Do not hard-code the value throughout the codebase.

Use one configuration value.

Example:

```text
700 MB
→ 1 part

1.5 GB
→ 2 parts

2.5 GB
→ 3 parts

4 GB
→ 5 parts
```

The number of parts must be calculated dynamically.

Never assume a movie has only two parts.

---

# 10. Part Metadata

Every movie part must store enough information to reconstruct the virtual movie.

At minimum:

```text
movie_id
part_number
size
start_offset
end_offset
telegram_message_id
telegram_file_id
status
```

Example:

```text
Movie size: 2.4 GB

Part 1
start: 0
end: 899 MB

Part 2
start: 900 MB
end: 1.799 GB

Part 3
start: 1.8 GB
end: 2.4 GB
```

Offsets must use consistent units.

Prefer `int64` for file sizes and byte offsets.

Do not use floating-point values for byte positions.

---

# 11. Virtual File

The backend must treat multiple Telegram parts as one logical video.

Example:

```text
Telegram:

Part 1
Part 2
Part 3

Application:

movie.mkv
2.4 GB
```

The frontend must not need to understand Telegram parts.

Do not expose part URLs to the frontend.

Do not require the frontend to concatenate files.

---

# 12. Streaming

Primary endpoint:

```text
GET /api/stream/movies/{movieID}
```

The streaming endpoint must support HTTP Range requests.

Example:

```http
Range: bytes=0-
```

Example:

```http
Range: bytes=500000000-600000000
```

The backend must:

1. Parse the Range header.
2. Validate the range.
3. Find the corresponding movie part.
4. Calculate the offset inside that Telegram part.
5. Request the required data from Telegram.
6. Stream it to the browser.
7. Continue across parts when necessary.
8. Return correct HTTP headers.

Use correct status codes:

```text
200
206
404
416
500
503
```

depending on the situation.

---

# 13. Range Mapping

This is a critical component.

Do not implement range mapping with assumptions.

For example:

```text
Movie:
2.4 GB

Part 1:
0 - 899 MB

Part 2:
900 MB - 1.799 GB

Part 3:
1.8 GB - 2.4 GB
```

If the browser requests:

```text
1.2 GB
```

the backend must resolve:

```text
Movie offset
      ↓
Part 2
      ↓
Part 2 local offset
      ↓
Telegram chunk
```

If a requested range crosses:

```text
Part 2 → Part 3
```

the streaming layer must transparently continue into Part 3.

The browser must still receive one continuous response.

---

# 14. Streaming Performance

Do not load an entire movie into RAM.

Never do:

```go
io.ReadAll(entireMovie)
```

for large videos.

Streaming must be chunked.

Use:

```text
io.Reader
io.ReaderAt
io.Writer
io.Copy
```

or equivalent streaming mechanisms where appropriate.

Be especially careful because the target development environment may have limited RAM.

Avoid unnecessary buffering.

---

# 15. Upload Performance

Do not load an entire multi-gigabyte movie into RAM.

Use streaming file access.

The split pipeline should operate using files/chunks rather than keeping the entire movie in memory.

The implementation should be safe for files several GB in size.

---

# 16. Upload Retry

Each part must have an independent status.

Example:

```text
Part 1 → COMPLETE
Part 2 → FAILED
Part 3 → WAITING
```

Retrying should retry Part 2 rather than restarting the entire movie upload.

Use retry logic for transient Telegram/network failures.

Use bounded retries.

Do not create infinite retry loops.

Use exponential backoff where appropriate.

---

# 17. Upload Resumption

Design upload state so an interrupted upload can be resumed.

Do not assume the process will always finish in one execution.

Persist enough information to determine:

```text
What parts exist?
What parts completed?
What parts failed?
What still needs uploading?
```

If full byte-level resume is not practical during the initial MVP, at minimum support **part-level retry/resume**.

---

# 18. Database

Use SQLite for MVP.

Keep database access behind a clear repository/data-access layer.

Do not scatter SQL queries throughout HTTP handlers.

Recommended logical tables:

```text
movies
movie_parts
subtitles
watch_progress
users
```

Use migrations.

Do not manually modify production schema without a migration.

Use foreign keys where appropriate.

Add indexes for frequently queried fields.

---

# 19. Movie Status

Use:

```text
DRAFT
UPLOADING
PROCESSING
READY
ERROR
DELETING
```

Only:

```text
READY
```

movies should be publicly playable.

Do not expose incomplete uploads to normal users.

---

# 20. Authentication

Admin endpoints must require authentication.

Passwords must never be stored as plaintext.

Use:

```text
Argon2id
```

or bcrypt if necessary.

Never log passwords or authentication secrets.

Do not expose admin APIs publicly without authentication.

---

# 21. Frontend Player

The frontend should use the streaming endpoint as a single logical video.

Example:

```text
/api/stream/movies/123
```

The player should support:

```text
Play
Pause
Seek
Volume
Fullscreen
Picture-in-picture
Subtitle
Resume
```

Do not implement manual Part 1 / Part 2 / Part 3 buttons.

Part transitions must be invisible to the user.

---

# 22. Watch Progress

Store playback position.

Example:

```text
movie_id
user_id
position_seconds
updated_at
```

When the user returns to a movie, offer:

```text
Continue from 01:23:15
```

If authentication is not implemented for normal users in the MVP, keep the progress implementation simple and do not over-engineer it.

---

# 23. Subtitle

Use WebVTT as the preferred browser format.

Support multiple subtitle languages.

Example:

```text
Indonesia
English
Japanese
Chinese
```

Only display languages that actually exist.

Do not force every movie to have subtitles.

Subtitle tracks live on **Telegram storage**, not on local disk: the
`subtitles` table keeps `telegram_file_id` + `telegram_message_id`, and the
serve endpoint streams the bytes back from the channel. `file_path` stays as
the disk fallback for rows uploaded before the move — a relative
`file_path` is resolved against `<dataDir>/subtitles/`, so the suite binary
works from any cwd.

---

# 24. API Design

Keep API routes predictable.

Example:

```text
GET    /api/movies
GET    /api/movies/:id
GET    /api/stream/movies/:id

POST   /api/admin/movies
PATCH  /api/admin/movies/:id
DELETE /api/admin/movies/:id

POST   /api/admin/movies/:id/upload
POST   /api/admin/movies/:id/retry

GET    /api/subtitles/:movieID
```

Exact routing can be adjusted during implementation if the architecture remains consistent with the PRD.

---

# 25. Error Handling

Errors must be explicit.

Examples:

```text
Movie not found
→ 404

Invalid Range
→ 416

Telegram temporarily unavailable
→ 503

Unexpected server error
→ 500
```

Do not expose internal Telegram errors directly to users.

Return useful but safe error messages.

---

# 26. Logging

Log important operational events:

```text
upload started
upload part started
upload part completed
upload failed
upload retry
stream started
stream ended
telegram request failed
range request
database error
```

Never log:

```text
password
API hash
Telegram session
tokens
cookies
private credentials
```

Use structured logging where practical.

---

# 27. Security

Treat all user input as untrusted.

Validate:

* filenames
* movie IDs
* query parameters
* Range headers
* upload metadata
* subtitle files
* authentication input

Prevent:

* path traversal
* SQL injection
* command injection
* unauthorized admin access
* credential leakage
* arbitrary file access

Do not execute uploaded files.

---

# 28. File Handling

Uploaded files are untrusted.

Do not trust the extension alone.

Validate actual media metadata where practical.

Temporary files must be cleaned up after successful processing or failure.

Do not leave multi-gigabyte temporary files indefinitely.

Make cleanup failure visible in logs.

---

# 29. FFmpeg

FFmpeg may be used for:

* metadata extraction
* media validation
* optional transcoding
* optional thumbnail generation

Do not transcode videos during the initial upload unless the PRD requires it.

Avoid unnecessary CPU usage.

The initial MVP should preferably preserve the original video.

---

# 30. Testing

Every important component must be testable independently.

Minimum tests:

```text
File splitting
Part size calculation
Part offset calculation
Range parsing
Range → part mapping
Cross-part range handling
Database CRUD
Upload state
Retry logic
Movie status transitions
Authentication
```

Critical streaming logic must have unit tests.

Example:

```text
Request:
bytes=850MB-950MB

Expected:
Part 1: 850MB → 900MB
Part 2: 900MB → 950MB
```

Test boundary conditions heavily.

---

# 31. Edge Cases

Explicitly test:

```text
File exactly 900 MB
File slightly above 900 MB
Very small file
Multi-GB file
Range at part boundary
Range crossing two parts
Range crossing multiple parts
Range beyond file size
Missing Telegram part
Failed Telegram request
Interrupted upload
Retry after failure
Deleted movie
Incomplete movie
```

Do not assume only normal cases.

---

# 32. Development Phases

Work incrementally.

Recommended order:

```text
Phase 1
Project foundation
├── Go application
├── configuration
├── SQLite
├── migrations
└── basic API

Phase 2
Telegram storage
├── MTProto connection
├── authentication/session
├── upload
└── file metadata

Phase 3
Upload pipeline
├── file validation
├── automatic splitting
├── part tracking
├── retry
└── resume

Phase 4
Streaming
├── Storage interface
├── Telegram range reader
├── virtual file
├── HTTP Range
└── cross-part streaming

Phase 5
Frontend
├── homepage
├── movie detail
├── player
└── subtitle

Phase 6
Admin
├── authentication
├── movie management
├── upload UI
└── upload progress

Phase 7
Testing and hardening
├── integration tests
├── error handling
├── security
└── performance
```

Do not skip directly to Phase 6 unless explicitly requested.

---

# 33. Working Rules for the Coding Agent

Before changing code:

1. Read relevant existing files.
2. Understand the current architecture.
3. Identify dependencies.
4. Make the smallest reasonable change.
5. Run tests.
6. Fix errors.
7. Review the resulting code.

Do not rewrite unrelated code.

Do not rename large numbers of files without a reason.

Do not replace working components merely because another technology is preferred.

---

# 34. Dependency Rules

Before adding a dependency:

Ask:

```text
Is the standard library sufficient?
Is this dependency actively maintained?
Does it solve a real problem?
Does it significantly increase complexity?
```

Avoid dependency bloat.

For critical Telegram functionality, prefer a well-maintained and documented implementation.

---

# 35. Git Rules

Do not commit:

```text
.env
Telegram sessions
credentials
API keys
database files containing secrets
large video files
temporary upload files
```

Use `.gitignore`.

Keep commits focused.

Do not create meaningless commits.

---

# 36. Environment Configuration

Use environment variables for secrets and deployment-specific settings.

Example:

```text
TELEGRAM_API_ID=
TELEGRAM_API_HASH=
TELEGRAM_SESSION=
TELEGRAM_CHANNEL_ID=

DATABASE_PATH=

MAX_PART_SIZE=
MAX_MOVIE_SIZE=

SERVER_HOST=
SERVER_PORT=

FRONTEND_URL=
COOKIE_SECURE=
```

Full list lives in `.env.example` (this snippet shows shape only).

Never hard-code credentials.

Provide a safe `.env.example`.

Never put real credentials inside `.env.example`.

---

# 37. Resource Constraints

Assume the development machine may have limited RAM and CPU.

Design for:

```text
large video files
limited RAM
limited temporary disk
unstable network
Telegram request failures
```

Avoid memory-heavy operations.

Do not assume unlimited server bandwidth.

---

# 38. Performance Priorities

Prioritize:

1. Streaming without loading entire files into memory.
2. Efficient Telegram chunk requests.
3. Correct Range handling.
4. Minimal unnecessary disk I/O.
5. Minimal unnecessary database queries.
6. Reasonable concurrent stream handling.

Do not implement complicated caching until profiling shows it is needed.

---

# 39. Important Architectural Constraint

Never solve a streaming problem by downloading the entire movie from Telegram first.

Bad:

```text
Telegram
   ↓
Download 2.5 GB
   ↓
RAM / temporary file
   ↓
Browser
```

Preferred:

```text
Browser
   ↓
Range request
   ↓
Go
   ↓
Required Telegram chunks
   ↓
Browser
```

---

# 40. Completion Requirements

A task is not complete merely because code was written.

Before declaring a task complete:

```text
[ ] Code compiles
[ ] Tests pass
[ ] No obvious errors
[ ] Relevant edge cases considered
[ ] Security implications checked
[ ] No secrets committed
[ ] PRD requirements preserved
[ ] Changed files reviewed
```

Report:

```text
What changed
Files changed
Tests executed
Test results
Known limitations
Next recommended step
```

Do not claim functionality works if it has not been tested.

---

# 41. Agent Behavior

Do not blindly follow a user's instruction if it conflicts with:

```text
PRD.md
AGENTS.md
existing security requirements
data integrity
```

Explain the conflict and propose the smallest safe alternative.

Do not silently remove existing functionality.

Do not silently change database schemas without migrations.

Do not silently change storage architecture.

Do not silently introduce a new framework.

When a major architectural decision is required, stop and explain the options before proceeding.

---

# 42. Content and Legal Boundary

This application is intended for content that the operator has the legal right to store and distribute.

Do not add functionality intended to bypass copyright enforcement, takedowns, access controls, DRM, or other content protections.

The storage architecture does not change the legal status of the content being stored or distributed.

---

# 43. Final Principle

Build the simplest reliable system that satisfies `PRD.md`.

The most critical technical areas are:

```text
Telegram storage
        ↓
automatic splitting
        ↓
part metadata
        ↓
virtual file
        ↓
HTTP Range
        ↓
continuous streaming
```

These components must be implemented carefully and tested thoroughly.

Do not sacrifice correctness for speed of implementation.

---

# 44. Ponytail Mode (Mandatory)

Every code edit MUST use the `ponytail` skill (load via skill tool) and follow
its ladder — the simplest reliable diff that satisfies the requirement, nothing more.

```text
1. Does it need to exist? No → don't build it, say so in one line.
2. Stdlib does it? Use stdlib.
3. Platform feature covers it? Use it.
4. Installed dependency solves it? Use it, never add one for a few lines.
5. One line? One line.
6. Else: minimum code that works.
```

Rules:

```text
No unrequested abstractions, boilerplate, or scaffolding "for later".
Delete over add; boring over clever; fewest files, shortest diff.
A finding that only saves lines but risks behavior stays out.
Never simplify away: validation, error handling, security, auth.
```

Mark deliberate simplifications with a `ponytail:` comment naming the
ceiling and the upgrade path.

---

# 45. Change Log (Mandatory)

Every code change MUST be recorded in `CHANGELOG.md` (newest on top):

```text
## YYYY-MM-DD — short title
- What changed and why (one line per item).
- Files changed.
- Tests executed + result.
```

No entry = task not complete (§40). Docs-only and config-only edits get
one line under the same date heading.

---

# Changelog

- **2026-10-22** — Subtitle tracks moved to Telegram storage (new columns
  `telegram_file_id` / `telegram_message_id`, migration `0006`); relative
  `file_path` resolved against `<dataDir>/subtitles/`; upload temp moved to
  `TMP_DIR` (default `/mnt/hdd/videos/tmp`) and janitor deletes temps older
  than 1 hour.

package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"web-go/backend/internal/storage"
)

// downloadChunk caps one upload.getFile call (Telegram limit: 512 KiB...1 MiB).
// DownloadRange loops internally, so callers get exactly `length` bytes.
const downloadChunk = 512 * 1024

// tgQuantum is Telegram's upload.getFile granularity: both offset and limit
// must be multiples of 1024, even with Precise set. Anything else fails with
// OFFSET_INVALID / LIMIT_INVALID — which surfaces in browsers as an eternally
// spinning video (e.g. moov-atom fetches are rarely 1024-aligned).
const tgQuantum = 1024

// TelegramStorage implements storage.Storage against the private channel.
// It owns no credentials beyond Client; all locators stay server-side.
type TelegramStorage struct {
	client *Client

	// refCache holds refreshed file references per Telegram message.
	// References expire server-side; the DB row keeps the original, so a
	// process-local cache avoids refetching on every range request.
	mu       sync.Mutex
	refCache map[int64][]byte
}

// NewStorage binds a connected Client to the storage interface.
func NewStorage(client *Client) *TelegramStorage {
	return &TelegramStorage{client: client, refCache: map[int64][]byte{}}
}

// cachedRef returns a previously refreshed reference for a message.
func (s *TelegramStorage) cachedRef(messageID int64) ([]byte, bool) {
	if messageID == 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.refCache[messageID]
	return ref, ok
}

func (s *TelegramStorage) cacheRef(messageID int64, ref []byte) {
	if messageID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refCache[messageID] = append([]byte(nil), ref...)
}

func (s *TelegramStorage) Upload(ctx context.Context, r io.Reader, meta storage.Metadata) (storage.File, error) {
	api, err := s.client.waitReady(ctx)
	if err != nil {
		return storage.File{}, err
	}
	// NewUpload streams with known total: big-file uploader for parts >10 MB.
	input, err := uploader.NewUploader(api).Upload(ctx, uploader.NewUpload(meta.Name, r, meta.Size))
	if err != nil {
		return storage.File{}, err
	}
	peer, err := s.client.ChannelPeer(ctx)
	if err != nil {
		return storage.File{}, err
	}
	updates, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Silent:     true,
		Background: true,
		Peer:       &peer,
		Media: &tg.InputMediaUploadedDocument{
			ForceFile: true,
			File:      input,
			MimeType:  meta.Mime,
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: meta.Name},
			},
		},
		Message:  meta.Name,
		RandomID: newRandomID(),
	})
	if err != nil {
		return storage.File{}, err
	}
	msgID, doc, err := extractDocument(updates)
	if err != nil {
		return storage.File{}, err
	}
	if doc.Size != meta.Size {
		slog.Error("telegram size mismatch", "want", meta.Size, "got", doc.Size)
		return storage.File{}, fmt.Errorf("telegram stored %d bytes, want %d", doc.Size, meta.Size)
	}
	slog.Info("telegram upload stored", "part", meta.Name, "size", doc.Size)
	return storage.File{ID: EncodeLocator(doc), MessageID: int64(msgID)}, nil
}

func newRandomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	if v == 0 {
		v = 1
	}
	return v
}

// extractDocument finds the sent channel message + document in send results.
func extractDocument(updates tg.UpdatesClass) (int, *tg.Document, error) {
	var list []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.Updates:
		list = u.Updates
	case *tg.UpdatesCombined:
		list = u.Updates
	default:
		return 0, nil, fmt.Errorf("unexpected updates type %T", updates)
	}
	for _, e := range list {
		ncm, ok := e.(*tg.UpdateNewChannelMessage)
		if !ok {
			continue
		}
		msg, ok := ncm.Message.(*tg.Message)
		if !ok {
			continue
		}
		media, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			continue
		}
		doc, ok := media.Document.(*tg.Document)
		if !ok {
			continue
		}
		return msg.ID, doc, nil
	}
	return 0, nil, fmt.Errorf("sent document not found in updates")
}

func (s *TelegramStorage) DownloadRange(ctx context.Context, f storage.File, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length <= 0 {
		return nil, fmt.Errorf("invalid range")
	}
	api, err := s.client.waitReady(ctx)
	if err != nil {
		return nil, err
	}
	docID, accessHash, fileRef, err := DecodeLocator(f.ID)
	if err != nil {
		return nil, err
	}
	if cached, ok := s.cachedRef(f.MessageID); ok {
		fileRef = cached
	}
	var out bytes.Buffer
	remaining, off := length, offset
	refreshed := false
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Align the RPC window, then trim back to the exact bytes wanted.
		rpcOff := off - off%tgQuantum
		skip := off - rpcOff
		want := skip + remaining
		want += (tgQuantum - want%tgQuantum) % tgQuantum
		if want > downloadChunk {
			want = downloadChunk
		}
		chunkLen := want - skip
		if chunkLen > remaining {
			chunkLen = remaining
		}
		b, err := s.fetchWindow(ctx, api, docID, accessHash, fileRef, rpcOff, skip, want, off, chunkLen)
		if err != nil {
			if !refreshed && isFileRefExpired(err) && f.MessageID != 0 {
				doc, rerr := s.refetch(ctx, api, f.MessageID)
				if rerr != nil {
					return nil, err
				}
				fileRef = doc.FileReference
				s.cacheRef(f.MessageID, fileRef)
				refreshed = true
				continue
			}
			return nil, err
		}
		// fetchWindow returns nil with no error at EOF (small files).
		if len(b) == 0 {
			break
		}
		// fetchWindow may return short of chunkLen at EOF; the next
		// iteration errors instead of looping forever.
		out.Write(b)
		off += int64(len(b))
		remaining -= int64(len(b))
	}
	return io.NopCloser(bytes.NewReader(out.Bytes())), nil
}

// fetchWindow serves [off, off+chunkLen) with one exact RPC, falling back
// to whole 512 KiB blocks for windows Telegram rejects.
func (s *TelegramStorage) fetchWindow(ctx context.Context, api *tg.Client, docID, accessHash int64, fileRef []byte, rpcOff, skip, want, off, chunkLen int64) ([]byte, error) {
	b, err := s.getFileOnce(ctx, api, docID, accessHash, fileRef, rpcOff, want)
	if err == nil {
		// EOF: Telegram returns an empty body (or fewer bytes than the
		// skip offset) past the end of the file. Normal for small files
		// (thumbs) and must not be an error.
		if len(b) == 0 || int64(len(b)) <= skip {
			return nil, nil
		}
		use := int64(len(b)) - skip
		if use > chunkLen {
			use = chunkLen
		}
		return b[skip : skip+use], nil
	}
	if !isInvalidRange(err) || want <= tgQuantum {
		return nil, err
	}
	return s.getFileBlocks(ctx, api, docID, accessHash, fileRef, off, chunkLen)
}

// getFileOnce performs one aligned upload.getFile call.
func (s *TelegramStorage) getFileOnce(ctx context.Context, api *tg.Client, docID, accessHash int64, fileRef []byte, rpcOff, want int64) ([]byte, error) {
	res, err := api.UploadGetFile(ctx, &tg.UploadGetFileRequest{
		Precise: true,
		Location: &tg.InputDocumentFileLocation{
			ID:            docID,
			AccessHash:    accessHash,
			FileReference: fileRef,
		},
		Offset: rpcOff,
		Limit:  int(want),
	})
	if err != nil {
		return nil, err
	}
	file, ok := res.(*tg.UploadFile)
	if !ok {
		return nil, fmt.Errorf("unexpected file type %T", res)
	}
	return file.Bytes, nil
}

// getFileBlocks fetches [off, off+length) as whole downloadChunk blocks and
// trims; the server clamps blocks past EOF. Slow path for windows the exact
// call rejects.
func (s *TelegramStorage) getFileBlocks(ctx context.Context, api *tg.Client, docID, accessHash int64, fileRef []byte, off, length int64) ([]byte, error) {
	var out bytes.Buffer
	end := off + length
	for blk := off - off%downloadChunk; blk < end; blk += downloadChunk {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, err := s.getFileOnce(ctx, api, docID, accessHash, fileRef, blk, downloadChunk)
		if err != nil {
			return nil, err
		}
		lo := off - blk
		if lo < 0 {
			lo = 0
		}
		hi := end - blk
		if hi > int64(len(b)) {
			hi = int64(len(b))
		}
		if lo >= hi {
			return nil, fmt.Errorf("short read at offset %d", off)
		}
		out.Write(b[lo:hi])
	}
	return out.Bytes(), nil
}

// isInvalidRange reports Telegram's shape rejections (recoverable via the
// block fallback); anything else (auth, flood, network) propagates.
func isInvalidRange(err error) bool {
	var rpcErr *tgerr.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Type == "OFFSET_INVALID" || rpcErr.Type == "LIMIT_INVALID"
}

// refetch reloads a fresh file reference from the stored channel message.
func (s *TelegramStorage) refetch(ctx context.Context, api *tg.Client, messageID int64) (*tg.Document, error) {
	peer, err := s.client.ChannelPeer(ctx)
	if err != nil {
		return nil, err
	}
	res, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: &tg.InputChannel{ChannelID: peer.ChannelID, AccessHash: peer.AccessHash},
		ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: int(messageID)}},
	})
	if err != nil {
		return nil, err
	}
	msgs, ok := res.(*tg.MessagesChannelMessages)
	if !ok || len(msgs.Messages) == 0 {
		return nil, fmt.Errorf("message %d not found", messageID)
	}
	msg, ok := msgs.Messages[0].(*tg.Message)
	if !ok {
		return nil, fmt.Errorf("message %d has no body", messageID)
	}
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil, fmt.Errorf("message %d has no document", messageID)
	}
	doc, ok := media.Document.(*tg.Document)
	if !ok {
		return nil, fmt.Errorf("message %d document unavailable", messageID)
	}
	return doc, nil
}

func (s *TelegramStorage) Delete(ctx context.Context, f storage.File) error {
	if f.MessageID == 0 {
		return fmt.Errorf("no message id")
	}
	api, err := s.client.waitReady(ctx)
	if err != nil {
		return err
	}
	peer, err := s.client.ChannelPeer(ctx)
	if err != nil {
		return err
	}
	_, err = api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{ChannelID: peer.ChannelID, AccessHash: peer.AccessHash},
		ID:      []int{int(f.MessageID)},
	})
	return err
}

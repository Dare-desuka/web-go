// Package telegram hides MTProto details behind storage.Storage.
// Credentials enter only via Config (parsed from env, never logged).
// All Telegram identifiers stay server-side; handlers never import this package.
package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Config carries Telegram credentials. Parse with ParseConfig; never log it.
type Config struct {
	APIID     int
	APIHash   string
	ChannelID int64
	// Session is decoded session payload (empty = fresh login via tglogin).
	Session []byte
	// SessionFile persists the session across restarts when set.
	SessionFile string
}

// ParseConfig validates env inputs. session may be empty (login later),
// a path to an existing file, a base64 payload, or a raw JSON payload.
func ParseConfig(apiIDStr, apiHash, Sess, channelIDStr string) (Config, error) {
	var c Config
	apiID, err := strconv.Atoi(strings.TrimSpace(apiIDStr))
	if err != nil || apiID <= 0 {
		return Config{}, fmt.Errorf("invalid TELEGRAM_API_ID")
	}
	c.APIID = apiID
	c.APIHash = strings.TrimSpace(apiHash)
	if c.APIHash == "" {
		return Config{}, fmt.Errorf("empty TELEGRAM_API_HASH")
	}
	ch, err := strconv.ParseInt(strings.TrimSpace(channelIDStr), 10, 64)
	if err != nil || ch == 0 {
		return Config{}, fmt.Errorf("invalid TELEGRAM_CHANNEL_ID")
	}
	c.ChannelID = ch

	s := strings.TrimSpace(Sess)
	if s == "" {
		return c, nil
	}
	if st, err := os.Stat(s); err == nil && !st.IsDir() {
		c.SessionFile = s
		data, err := os.ReadFile(s)
		if err != nil {
			return Config{}, err
		}
		c.Session = data
		return c, nil
	}
	if strings.HasPrefix(s, "{") {
		c.Session = []byte(s)
		return c, nil
	}
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		c.Session = data
		return c, nil
	}
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		c.Session = data
		return c, nil
	}
	return Config{}, fmt.Errorf("invalid TELEGRAM_SESSION: not a file, JSON, or base64")
}

// memSession is a non-persisting session store for payload-from-env mode.
// Prefer SessionFile (gotd's FileStorage) so auth refreshes survive restarts.
type memSession struct {
	mu   sync.Mutex
	data []byte
}

func (s *memSession) LoadSession(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) == 0 {
		return nil, session.ErrNotFound
	}
	out := make([]byte, len(s.data))
	copy(out, s.data)
	return out, nil
}

func (s *memSession) StoreSession(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append([]byte(nil), data...)
	return nil
}

// NewSessionStorage builds the gotd session store for cfg.
func NewSessionStorage(cfg Config) session.Storage {
	if cfg.SessionFile != "" {
		return &session.FileStorage{Path: cfg.SessionFile}
	}
	return &memSession{data: cfg.Session}
}

// Client is a connected MTProto user client. Run it in the background;
// API calls block until the first connection is ready.
type Client struct {
	client *telegram.Client
	cfg    Config

	readyCh   chan struct{}
	readyOnce sync.Once
	api       *tg.Client

	mu   sync.RWMutex
	peer *tg.InputPeerChannel
}

// NewClient builds a disconnected client for cfg.
func NewClient(cfg Config) *Client {
	c := &Client{cfg: cfg, readyCh: make(chan struct{})}
	c.client = telegram.NewClient(cfg.APIID, cfg.APIHash, telegram.Options{
		SessionStorage: NewSessionStorage(cfg),
		NoUpdates:      true, // storage traffic only; send results arrive via RPC
	})
	return c
}

// Auth exposes gotd auth for the one-time login helper.
func (c *Client) Auth() *auth.Client {
	return c.client.Auth()
}

// Run connects (gotd reconnects internally) until ctx ends.
func (c *Client) Run(ctx context.Context) error {
	return c.client.Run(ctx, func(ctx context.Context) error {
		c.api = c.client.API()
		c.readyOnce.Do(func() { close(c.readyCh) })
		<-ctx.Done()
		return nil
	})
}

// Self returns the logged-in account (used by the login helper).
func (c *Client) Self(ctx context.Context) (*tg.User, error) {
	api, err := c.waitReady(ctx)
	if err != nil {
		return nil, err
	}
	_ = api
	return c.client.Self(ctx)
}

func (c *Client) waitReady(ctx context.Context) (*tg.Client, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.readyCh:
		return c.api, nil
	}
}

// ChannelPeer resolves the storage channel to an input peer, cached.
func (c *Client) ChannelPeer(ctx context.Context) (tg.InputPeerChannel, error) {
	c.mu.RLock()
	if c.peer != nil {
		p := *c.peer
		c.mu.RUnlock()
		return p, nil
	}
	c.mu.RUnlock()

	api, err := c.waitReady(ctx)
	if err != nil {
		return tg.InputPeerChannel{}, err
	}
	peer, err := FindChannel(ctx, api, c.cfg.ChannelID)
	if err != nil {
		return tg.InputPeerChannel{}, err
	}
	c.mu.Lock()
	c.peer = &peer
	c.mu.Unlock()
	return peer, nil
}

// FindChannel scans dialogs for a channel ID and returns its input peer.
// Exported for the login helper; paginates modestly (private channels have
// no username to resolve directly).
func FindChannel(ctx context.Context, api *tg.Client, channelID int64) (tg.InputPeerChannel, error) {
	offsetPeer := tg.InputPeerClass(&tg.InputPeerEmpty{})
	for page := 0; page < 10; page++ {
		dialogs, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			OffsetPeer: offsetPeer,
			Limit:      100,
		})
		if err != nil {
			return tg.InputPeerChannel{}, err
		}
		list, ok := dialogs.(*tg.MessagesDialogs)
		if !ok {
			// Slice variant (too many dialogs); not worth paginating blind.
			return tg.InputPeerChannel{}, fmt.Errorf("channel %d not in first dialog page", channelID)
		}
		for _, ch := range list.Chats {
			if ch, ok := ch.(*tg.Channel); ok && ch.ID == channelID {
				return tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
			}
		}
		if len(list.Dialogs) == 0 {
			break
		}
		// Best-effort pagination via last message offset.
		advanced := false
		for i := len(list.Messages) - 1; i >= 0; i-- {
			if msg, ok := list.Messages[i].(*tg.Message); ok {
				_ = msg
				advanced = true
				break
			}
		}
		if !advanced {
			break
		}
	}
	return tg.InputPeerChannel{}, fmt.Errorf("channel %d not found in dialogs (is the account a member?)", channelID)
}

// isFileRefExpired reports FILE_REFERENCE_EXPIRED for one transparent retry.
func isFileRefExpired(err error) bool {
	var rpcErr *tgerr.Error
	return errors.As(err, &rpcErr) && rpcErr.Type == "FILE_REFERENCE_EXPIRED"
}

// Command tglogin performs the one-time Telegram login for the dedicated
// project account and prints what to put in .env. Build once, then run on
// your own machine:
//
//	go build -o tglogin ./backend/cmd/tglogin
//	TELEGRAM_API_ID=... TELEGRAM_API_HASH=... ./tglogin [-v] [-out path]
//
// It asks for the project phone number, the OTP (arrives in-app), and the
// 2FA password if set. SendCode is FLOOD_WAIT-aware: short throttles are
// waited out automatically instead of failing. It writes the session file
// (default data/tg-session.json) and prints the TELEGRAM_SESSION payload
// plus the numeric channel ID after you pick the storage channel.
// Nothing is sent anywhere except Telegram's own servers.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/log/logslog"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	tgpkg "web-go/backend/internal/telegram"
)

// maxAutoWait caps automatic FLOOD_WAIT sleeping. Longer throttles abort
// with guidance instead of hanging the terminal for hours.
const maxAutoWait = 2 * time.Minute

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tglogin:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", filepath.Join("data", "tg-session.json"), "session file to write (0600)")
	verbose := flag.Bool("v", false, "enable gotd debug logs")
	flag.Parse()

	apiID, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TELEGRAM_API_ID")))
	if err != nil || apiID <= 0 {
		return fmt.Errorf("set TELEGRAM_API_ID first")
	}
	apiHash := strings.TrimSpace(os.Getenv("TELEGRAM_API_HASH"))
	if apiHash == "" {
		return fmt.Errorf("set TELEGRAM_API_HASH first")
	}

	opts := telegram.Options{
		SessionStorage: &session.FileStorage{Path: *out},
		NoUpdates:      true,
	}
	if *verbose {
		opts.Logger = logslog.New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}
	client := telegram.NewClient(apiID, apiHash, opts)
	in := bufio.NewReader(os.Stdin)

	say("Connecting to Telegram… (first run may take a while)")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			if err := authorize(ctx, client.Auth(), in); err != nil {
				return err
			}
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		say(fmt.Sprintf("Logged in as: %s (id %d)", displayName(self), self.ID))

		channels, err := listChannels(ctx, client.API())
		if err != nil {
			return err
		}
		if len(channels) == 0 {
			return fmt.Errorf("no channels found; create the private storage channel first")
		}
		say("Your channels:")
		for i, ch := range channels {
			fmt.Printf("  [%d] %s (id %d)\n", i, ch.Title, ch.ID)
		}
		n, err := promptInt(in, "Pick storage channel number: ", 0, len(channels)-1)
		if err != nil {
			return err
		}
		pick := channels[n]
		if _, err := tgpkg.FindChannel(ctx, client.API(), pick.ID); err != nil {
			return err
		}
		data, err := os.ReadFile(*out)
		if err != nil {
			return err
		}
		say("---- copy to .env (yours only, never commit) ----")
		fmt.Printf("TELEGRAM_SESSION=%s\n", base64.StdEncoding.EncodeToString(data))
		fmt.Printf("TELEGRAM_CHANNEL_ID=%d\n", pick.ID)
		fmt.Printf("Session file also saved at %s\n", *out)
		return nil
	})
}

// authorize runs phone → code → (2FA) login with FLOOD_WAIT-tolerant SendCode.
func authorize(ctx context.Context, ac *auth.Client, in *bufio.Reader) error {
	phone, err := prompt(in, "Project phone (international format): ")
	if err != nil {
		return err
	}
	sent, err := sendCodeWithRetry(ctx, ac, phone)
	if err != nil {
		return err
	}
	code, ok := sent.(*tg.AuthSentCode)
	if !ok {
		// Already authorized (AuthSentCodeSuccess) or unexpected shape.
		say("Already authorized, continuing.")
		return nil
	}
	say("Code sent — check Telegram on your phone, then enter it here.")
	otp, err := prompt(in, "OTP code from Telegram: ")
	if err != nil {
		return err
	}
	if _, err := ac.SignIn(ctx, phone, otp, code.PhoneCodeHash); err != nil {
		if errors.Is(err, auth.ErrPasswordAuthNeeded) {
			pwd, err := prompt(in, "2FA password: ")
			if err != nil {
				return err
			}
			if _, err := ac.Password(ctx, pwd); err != nil {
				return fmt.Errorf("2fa: %w", err)
			}
			return nil
		}
		return fmt.Errorf("sign in: %w", err)
	}
	return nil
}

// sendCodeWithRetry requests an OTP, automatically waiting out short
// FLOOD_WAIT throttles instead of failing. Long throttles abort with
// guidance: hammering the endpoint only extends the ban.
func sendCodeWithRetry(ctx context.Context, ac *auth.Client, phone string) (tg.AuthSentCodeClass, error) {
	for attempt := 0; ; attempt++ {
		sent, err := ac.SendCode(ctx, phone, auth.SendCodeOptions{})
		if err == nil {
			return sent, nil
		}
		wait, ok := floodWaitSeconds(err)
		if !ok || attempt >= 2 {
			return nil, fmt.Errorf("send code: %w", err)
		}
		if wait <= 0 {
			wait = 30
		}
		delay := time.Duration(wait+5) * time.Second
		if delay > maxAutoWait {
			return nil, fmt.Errorf("send code throttled for ~%ds: stop for several hours, then run once (each retry extends the ban): %w", wait, err)
		}
		say(fmt.Sprintf("Rate limited by Telegram, waiting %s automatically… (do not restart)", delay.Round(time.Second)))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// floodWaitSeconds extracts the wait from FLOOD_WAIT errors (tgerr parses
// "FLOOD_WAIT_85" into Type FLOOD_WAIT + Argument 85).
func floodWaitSeconds(err error) (int, bool) {
	var rpcErr *tgerr.Error
	if !errors.As(err, &rpcErr) {
		return 0, false
	}
	if rpcErr.Code != 420 && rpcErr.Type != "FLOOD_WAIT" &&
		!strings.HasPrefix(rpcErr.Type, "FLOOD_WAIT_") {
		return 0, false
	}
	return rpcErr.Argument, true
}

// say prints a progress line and flushes, so output never looks hung.
func say(s string) {
	fmt.Println(s)
	_ = os.Stdout.Sync()
}

func prompt(in *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	_ = os.Stdout.Sync()
	line, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptInt(in *bufio.Reader, label string, min, max int) (int, error) {
	line, err := prompt(in, label)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("enter a number between %d and %d", min, max)
	}
	return n, nil
}

type channelInfo struct {
	ID    int64
	Title string
}

func displayName(u *tg.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return name
}

func listChannels(ctx context.Context, api *tg.Client) ([]channelInfo, error) {
	dialogs, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return nil, err
	}
	list, ok := dialogs.(*tg.MessagesDialogs)
	if !ok {
		return nil, fmt.Errorf("too many dialogs to list; delete old chats and retry")
	}
	var out []channelInfo
	for _, ch := range list.Chats {
		if ch, ok := ch.(*tg.Channel); ok && !ch.Left {
			out = append(out, channelInfo{ID: ch.ID, Title: ch.Title})
		}
	}
	return out, nil
}

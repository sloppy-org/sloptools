package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/store"
)

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	*s = append(*s, clean)
	return nil
}

func cmdMail(args []string) int {
	if len(args) == 0 {
		printMailHelp()
		return 2
	}
	switch args[0] {
	case "send":
		return cmdMailSend(args[1:])
	case "reply":
		return cmdMailReply(args[1:])
	case "help", "-h", "--help":
		printMailHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown mail subcommand: %s\n", args[0])
		printMailHelp()
		return 2
	}
}

func printMailHelp() {
	fmt.Println("sloptools mail <send|reply> [flags]")
	fmt.Println()
	fmt.Println("send flags:")
	fmt.Println("  --account-id N      external account id to send from (required)")
	fmt.Println("  --to ADDR           recipient, repeatable (required)")
	fmt.Println("  --cc ADDR           cc recipient, repeatable")
	fmt.Println("  --bcc ADDR          bcc recipient, repeatable")
	fmt.Println("  --subject TEXT      subject line (required)")
	fmt.Println("  --body TEXT         inline body text (or use --body-file)")
	fmt.Println("  --body-file PATH    read body from file; - for stdin")
	fmt.Println("  --in-reply-to ID    optional In-Reply-To header")
	fmt.Println("  --references ID     optional References header, repeatable")
	fmt.Println("  --attach PATH       attach file from disk, repeatable")
	fmt.Println("  --draft             save as draft without sending")
	fmt.Println()
	fmt.Println("reply flags:")
	fmt.Println("  --account-id N      external account id to reply from (required)")
	fmt.Println("  --message-id ID     provider message id to reply to (required)")
	fmt.Println("  --body TEXT         inline body text (or use --body-file)")
	fmt.Println("  --body-file PATH    read body from file; - for stdin")
	fmt.Println("  --quote-style S     bottom_post (GCC / default) or top_post (business)")
	fmt.Println("  --reply-all         include original Cc recipients")
	fmt.Println("  --to ADDR           override To, repeatable")
	fmt.Println("  --cc ADDR           additional cc, repeatable")
	fmt.Println("  --bcc ADDR          bcc recipient, repeatable")
	fmt.Println("  --subject TEXT      override subject")
	fmt.Println("  --attach PATH       attach file from disk, repeatable")
	fmt.Println("  --draft             save as draft without sending")
}

type mailCommonFlags struct {
	accountID   int64
	to          stringListFlag
	cc          stringListFlag
	bcc         stringListFlag
	subject     string
	body        string
	bodyFile    string
	attachments stringListFlag
	draft       bool
	dataDir     string
	projectDir  string
}

func bindCommonFlags(fs *flag.FlagSet, c *mailCommonFlags) {
	fs.Int64Var(&c.accountID, "account-id", 0, "external account id")
	fs.Var(&c.to, "to", "recipient (repeatable)")
	fs.Var(&c.cc, "cc", "cc recipient (repeatable)")
	fs.Var(&c.bcc, "bcc", "bcc recipient (repeatable)")
	fs.StringVar(&c.subject, "subject", "", "subject line")
	fs.StringVar(&c.body, "body", "", "inline body text")
	fs.StringVar(&c.bodyFile, "body-file", "", "read body from file (- for stdin)")
	fs.Var(&c.attachments, "attach", "attach file from disk (repeatable)")
	fs.BoolVar(&c.draft, "draft", false, "save as draft without sending")
	fs.StringVar(&c.dataDir, "data-dir", defaultDataDir(), "sloptools data dir")
	fs.StringVar(&c.projectDir, "project-dir", ".", "project dir")
}

func defaultDataDir() string {
	home := os.Getenv("HOME")
	return filepath.Join(home, ".local", "share", "sloptools")
}

func cmdMailSend(args []string) int {
	fs := flag.NewFlagSet("mail send", flag.ContinueOnError)
	var common mailCommonFlags
	var inReplyTo string
	var references stringListFlag
	bindCommonFlags(fs, &common)
	fs.StringVar(&inReplyTo, "in-reply-to", "", "In-Reply-To header value")
	fs.Var(&references, "references", "References header value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if common.accountID <= 0 {
		fmt.Fprintln(os.Stderr, "error: --account-id is required")
		return 2
	}
	if len(common.to) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one --to recipient is required")
		return 2
	}
	if strings.TrimSpace(common.subject) == "" {
		fmt.Fprintln(os.Stderr, "error: --subject is required")
		return 2
	}
	body, err := loadBody(common.body, common.bodyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	attachments, err := loadAttachments(common.attachments)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req := mcp.MailSendRequest{
		AccountID:   common.accountID,
		To:          common.to,
		Cc:          common.cc,
		Bcc:         common.bcc,
		Subject:     common.subject,
		Body:        body,
		InReplyTo:   strings.TrimSpace(inReplyTo),
		References:  references,
		Attachments: attachments,
		DraftOnly:   common.draft,
	}
	return runMailSend(common, req)
}

func cmdMailReply(args []string) int {
	fs := flag.NewFlagSet("mail reply", flag.ContinueOnError)
	var common mailCommonFlags
	var messageID string
	var quoteStyle string
	var replyAll bool
	bindCommonFlags(fs, &common)
	fs.StringVar(&messageID, "message-id", "", "provider message id to reply to")
	fs.StringVar(&quoteStyle, "quote-style", "bottom_post", "quote style: bottom_post or top_post")
	fs.BoolVar(&replyAll, "reply-all", false, "also include original Cc recipients")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if common.accountID <= 0 {
		fmt.Fprintln(os.Stderr, "error: --account-id is required")
		return 2
	}
	if strings.TrimSpace(messageID) == "" {
		fmt.Fprintln(os.Stderr, "error: --message-id is required")
		return 2
	}
	body, err := loadBody(common.body, common.bodyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	style, err := email.ParseReplyQuoteStyle(quoteStyle)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	attachments, err := loadAttachments(common.attachments)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req := mcp.MailReplyRequest{
		AccountID:   common.accountID,
		MessageID:   strings.TrimSpace(messageID),
		Body:        body,
		QuoteStyle:  style,
		ReplyAll:    replyAll,
		To:          common.to,
		Cc:          common.cc,
		Bcc:         common.bcc,
		Subject:     common.subject,
		Attachments: attachments,
		DraftOnly:   common.draft,
	}
	return runMailReply(common, req)
}

func runMailSend(common mailCommonFlags, req mcp.MailSendRequest) int {
	srv, st, err := newMailServer(common.projectDir, common.dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	account, provider, err := srv.ResolveMailAccount(context.Background(), req.AccountID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve account: %v\n", err)
		return 1
	}
	defer provider.Close()
	result, err := mcp.ExecuteMailSend(context.Background(), account, provider, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return emitMailResult(result)
}

func runMailReply(common mailCommonFlags, req mcp.MailReplyRequest) int {
	srv, st, err := newMailServer(common.projectDir, common.dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	account, provider, err := srv.ResolveMailAccount(context.Background(), req.AccountID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve account: %v\n", err)
		return 1
	}
	defer provider.Close()
	result, err := mcp.ExecuteMailReply(context.Background(), account, provider, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return emitMailResult(result)
}

func newMailServer(projectDir, dataDir string) (*mcp.Server, *store.Store, error) {
	st, err := store.New(filepath.Join(dataDir, "sloptools.db"))
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	srv := mcp.NewServerWithStore(projectDir, st)
	return srv, st, nil
}

func emitMailResult(result mcp.MailComposeResult) int {
	payload := map[string]interface{}{
		"ok":        true,
		"draft_id":  result.DraftID,
		"thread_id": result.ThreadID,
		"sent":      result.Sent,
		"composed":  result.Composed,
	}
	if result.Reply != nil {
		payload["reply"] = result.Reply
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func loadBody(inline, file string) (string, error) {
	if strings.TrimSpace(inline) != "" && strings.TrimSpace(file) != "" {
		return "", fmt.Errorf("--body and --body-file are mutually exclusive")
	}
	if strings.TrimSpace(inline) != "" {
		return inline, nil
	}
	path := strings.TrimSpace(file)
	if path == "" {
		return "", fmt.Errorf("--body or --body-file is required")
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(data), nil
}

func loadAttachments(paths []string) ([]email.DraftAttachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]email.DraftAttachment, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", path, err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", abs, err)
		}
		ct := mime.TypeByExtension(filepath.Ext(abs))
		if ct == "" {
			ct = "application/octet-stream"
		}
		out = append(out, email.DraftAttachment{
			Filename:    filepath.Base(abs),
			ContentType: ct,
			Content:     data,
		})
	}
	return out, nil
}

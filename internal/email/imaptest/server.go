package imaptest

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// TestMessage represents a test email message.
type TestMessage struct {
	UID     uint32
	Subject string
	From    string
	To      string
	Date    time.Time
	Body    string
	Flags   []imap.Flag
}

// TestMailbox represents a test mailbox/folder.
type TestMailbox struct {
	Name     string
	Messages []TestMessage
}

// Server is an in-memory IMAP test server.
type Server struct {
	listener  net.Listener
	server    *imapserver.Server
	mailboxes map[string]*TestMailbox
	username  string
	password  string
	mu        sync.RWMutex
	done      chan struct{}
}

// NewServer creates a new test IMAP server.
func NewServer(username, password string) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	s := &Server{
		listener:  listener,
		mailboxes: make(map[string]*TestMailbox),
		username:  username,
		password:  password,
		done:      make(chan struct{}),
	}

	// Add default INBOX
	s.mailboxes["INBOX"] = &TestMailbox{Name: "INBOX"}

	server := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &testSession{server: s}, nil, nil
		},
		InsecureAuth: true,
	})
	s.server = server

	go func() {
		server.Serve(listener)
		close(s.done)
	}()

	return s, nil
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Port returns the server port.
func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Close shuts down the server.
func (s *Server) Close() error {
	err := s.listener.Close()
	<-s.done
	return err
}

// AddMailbox adds a mailbox to the server.
func (s *Server) AddMailbox(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailboxes[name] = &TestMailbox{Name: name}
}

// AddMessage adds a message to a mailbox.
func (s *Server) AddMessage(mailbox string, msg TestMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mbox, ok := s.mailboxes[mailbox]
	if !ok {
		mbox = &TestMailbox{Name: mailbox}
		s.mailboxes[mailbox] = mbox
	}

	if msg.UID == 0 {
		msg.UID = uint32(len(mbox.Messages) + 1)
	}
	if msg.Date.IsZero() {
		msg.Date = time.Now()
	}

	mbox.Messages = append(mbox.Messages, msg)
}

// testSession implements imapserver.Session for testing.
type testSession struct {
	server      *Server
	state       string // "not_auth", "auth", "selected"
	selectedBox string
}

func (s *testSession) Close() error {
	return nil
}

func (s *testSession) Login(username, password string) error {
	if username == s.server.username && password == s.server.password {
		s.state = "auth"
		return nil
	}
	return imapserver.ErrAuthFailed
}

func (s *testSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()

	mbox, ok := s.server.mailboxes[mailbox]
	if !ok {
		return nil, fmt.Errorf("mailbox not found: %s", mailbox)
	}

	s.state = "selected"
	s.selectedBox = mailbox

	return &imap.SelectData{
		NumMessages: uint32(len(mbox.Messages)),
		Flags:       []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft},
	}, nil
}

func (s *testSession) Unselect() error {
	s.state = "auth"
	s.selectedBox = ""
	return nil
}

func (s *testSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()

	for name := range s.server.mailboxes {
		for _, pattern := range patterns {
			if pattern == "*" || pattern == name || strings.Contains(name, strings.ReplaceAll(pattern, "*", "")) {
				w.WriteList(&imap.ListData{
					Mailbox: name,
					Delim:   '/',
				})
				break
			}
		}
	}
	return nil
}

func (s *testSession) Create(mailbox string, options *imap.CreateOptions) error {
	s.server.AddMailbox(mailbox)
	return nil
}

func (s *testSession) Delete(mailbox string) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	delete(s.server.mailboxes, mailbox)
	return nil
}

func (s *testSession) Rename(mailbox, newName string, options *imap.RenameOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	mbox, ok := s.server.mailboxes[mailbox]
	if !ok {
		return fmt.Errorf("mailbox not found: %s", mailbox)
	}
	delete(s.server.mailboxes, mailbox)
	mbox.Name = newName
	s.server.mailboxes[newName] = mbox
	return nil
}

func (s *testSession) Subscribe(mailbox string) error {
	return nil
}

func (s *testSession) Unsubscribe(mailbox string) error {
	return nil
}

func (s *testSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()

	mbox, ok := s.server.mailboxes[mailbox]
	if !ok {
		return nil, fmt.Errorf("mailbox not found: %s", mailbox)
	}

	numMessages := uint32(len(mbox.Messages))
	return &imap.StatusData{
		Mailbox:     mailbox,
		NumMessages: &numMessages,
	}, nil
}

func (s *testSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()

	mbox, ok := s.server.mailboxes[s.selectedBox]
	if !ok {
		return nil, fmt.Errorf("no mailbox selected")
	}

	var uids []imap.UID
	for _, msg := range mbox.Messages {
		if matchesCriteria(msg, criteria) {
			uids = append(uids, imap.UID(msg.UID))
		}
	}

	return &imap.SearchData{
		All: imap.UIDSetNum(uids...),
	}, nil
}

func matchesCriteria(msg TestMessage, criteria *imap.SearchCriteria) bool {
	if len(criteria.Text) > 0 {
		for _, text := range criteria.Text {
			if !strings.Contains(strings.ToLower(msg.Subject+msg.Body+msg.From), strings.ToLower(text)) {
				return false
			}
		}
	}

	for _, hdr := range criteria.Header {
		switch strings.ToLower(hdr.Key) {
		case "from":
			if !strings.Contains(strings.ToLower(msg.From), strings.ToLower(hdr.Value)) {
				return false
			}
		case "to":
			if !strings.Contains(strings.ToLower(msg.To), strings.ToLower(hdr.Value)) {
				return false
			}
		case "subject":
			if !strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(hdr.Value)) {
				return false
			}
		}
	}

	if !criteria.Since.IsZero() && msg.Date.Before(criteria.Since) {
		return false
	}

	if !criteria.Before.IsZero() && !msg.Date.Before(criteria.Before) {
		return false
	}

	return true
}

func (s *testSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()

	mbox, ok := s.server.mailboxes[s.selectedBox]
	if !ok {
		return fmt.Errorf("no mailbox selected")
	}

	for _, msg := range mbox.Messages {
		uid := imap.UID(msg.UID)
		if !containsUID(numSet, uid) {
			continue
		}

		respWriter := w.CreateMessage(uint32(msg.UID))

		if options.UID {
			respWriter.WriteUID(uid)
		}
		if options.Flags {
			respWriter.WriteFlags(msg.Flags)
		}
		if options.Envelope {
			respWriter.WriteEnvelope(buildEnvelope(msg))
		}
		if len(options.BodySection) > 0 {
			for _, section := range options.BodySection {
				body := buildRawMessage(msg)
				bodyWriter := respWriter.WriteBodySection(section, int64(len(body)))
				bodyWriter.Write([]byte(body))
				bodyWriter.Close()
			}
		}

		respWriter.Close()
	}

	return nil
}

func containsUID(numSet imap.NumSet, uid imap.UID) bool {
	uidSet, ok := numSet.(imap.UIDSet)
	if !ok {
		return false
	}
	return uidSet.Contains(uid)
}

func buildEnvelope(msg TestMessage) *imap.Envelope {
	return &imap.Envelope{
		Date:      msg.Date,
		Subject:   msg.Subject,
		From:      []imap.Address{parseAddr(msg.From)},
		To:        []imap.Address{parseAddr(msg.To)},
		MessageID: fmt.Sprintf("<%d@test>", msg.UID),
	}
}

func parseAddr(addr string) imap.Address {
	parts := strings.Split(addr, "@")
	if len(parts) != 2 {
		return imap.Address{Mailbox: addr, Host: "unknown"}
	}
	return imap.Address{Mailbox: parts[0], Host: parts[1]}
}

func buildRawMessage(msg TestMessage) string {
	return fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain\r\n\r\n%s",
		msg.From, msg.To, msg.Subject, msg.Date.Format(time.RFC1123Z), msg.Body)
}

func (s *testSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *testSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	return nil
}

func (s *testSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	<-stop
	return nil
}

func (s *testSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	src, ok := s.server.mailboxes[s.selectedBox]
	if !ok {
		return nil, fmt.Errorf("no mailbox selected")
	}

	dst, ok := s.server.mailboxes[dest]
	if !ok {
		dst = &TestMailbox{Name: dest}
		s.server.mailboxes[dest] = dst
	}

	srcUIDs := imap.UIDSet{}
	dstUIDs := imap.UIDSet{}
	for _, msg := range src.Messages {
		uid := imap.UID(msg.UID)
		if !containsUID(numSet, uid) {
			continue
		}

		srcUIDs.AddNum(uid)

		copied := msg
		copied.UID = uint32(len(dst.Messages) + 1)
		dst.Messages = append(dst.Messages, copied)
		dstUIDs.AddNum(imap.UID(copied.UID))
	}

	return &imap.CopyData{
		UIDValidity: 1,
		SourceUIDs:  srcUIDs,
		DestUIDs:    dstUIDs,
	}, nil
}

func (s *testSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	mbox, ok := s.server.mailboxes[s.selectedBox]
	if !ok {
		return fmt.Errorf("no mailbox selected")
	}

	for i := range mbox.Messages {
		uid := imap.UID(mbox.Messages[i].UID)
		if !containsUID(numSet, uid) {
			continue
		}

		switch flags.Op {
		case imap.StoreFlagsSet:
			mbox.Messages[i].Flags = append([]imap.Flag(nil), flags.Flags...)
		case imap.StoreFlagsAdd:
			for _, f := range flags.Flags {
				if !hasFlag(mbox.Messages[i].Flags, f) {
					mbox.Messages[i].Flags = append(mbox.Messages[i].Flags, f)
				}
			}
		case imap.StoreFlagsDel:
			mbox.Messages[i].Flags = removeFlags(mbox.Messages[i].Flags, flags.Flags)
		}
	}

	return nil
}

func (s *testSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	mbox, ok := s.server.mailboxes[s.selectedBox]
	if !ok {
		return fmt.Errorf("no mailbox selected")
	}

	newMessages := make([]TestMessage, 0, len(mbox.Messages))
	seqNum := uint32(1)

	for _, msg := range mbox.Messages {
		uid := imap.UID(msg.UID)
		matchesUID := uids == nil || uids.Contains(uid)
		shouldDelete := matchesUID && hasFlag(msg.Flags, imap.FlagDeleted)

		if shouldDelete {
			if err := w.WriteExpunge(seqNum); err != nil {
				return err
			}
			continue
		}

		newMessages = append(newMessages, msg)
		seqNum++
	}

	mbox.Messages = newMessages
	return nil
}

func hasFlag(flags []imap.Flag, target imap.Flag) bool {
	for _, f := range flags {
		if f == target {
			return true
		}
	}
	return false
}

func removeFlags(flags []imap.Flag, toRemove []imap.Flag) []imap.Flag {
	removeSet := make(map[imap.Flag]bool, len(toRemove))
	for _, f := range toRemove {
		removeSet[f] = true
	}

	var filtered []imap.Flag
	for _, f := range flags {
		if !removeSet[f] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// Command checkcapabilities probes the capability interfaces implemented by
// each enabled external account and prints a Markdown capability matrix.
//
// The output is designed to be compared against docs/groupware.md to verify
// that the documented capability matrix matches the actual provider
// implementations. Run:
//
//	go run ./cmd/checkcapabilities -store ~/.config/sloptools/sloptools.db
//
// The command constructs providers from the store and uses static type
// assertions (groupware.Supports[T]) to probe capability interfaces. It does
// not issue live network calls. Accounts that fail to resolve (missing
// credentials) are noted but do not cause a non-zero exit.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

// providerInfo holds the resolved providers for one external account.
type providerInfo struct {
	account  store.ExternalAccount
	mail     email.EmailProvider
	calendar calendar.Provider
	contacts contacts.Provider
	tasks    tasks.Provider
	mailbox  mailboxsettings.OOFProvider
	err      error // non-nil when construction failed
}

func main() {
	storePath := flag.String("store", "", "path to the SQLite store (default: ~/.config/sloptools/sloptools.db)")
	flag.Parse()
	if *storePath == "" {
		*storePath = defaultStorePath()
	}

	db, err := store.New(*storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	reg := groupware.NewRegistry(db, "")

	accounts, err := db.ListExternalAccounts("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "list accounts: %v\n", err)
		os.Exit(1)
	}

	var enabled []store.ExternalAccount
	for _, a := range accounts {
		if a.Enabled {
			enabled = append(enabled, a)
		}
	}

	if len(enabled) == 0 {
		fmt.Println("No enabled external accounts in store.")
		return
	}

	ctx := context.Background()

	var results []providerInfo
	for _, acct := range enabled {
		info := probeAccount(ctx, reg, acct)
		results = append(results, info)
	}

	printMatrix(results)
}

func probeAccount(ctx context.Context, reg *groupware.Registry, acct store.ExternalAccount) providerInfo {
	info := providerInfo{account: acct}
	switch acct.Provider {
	case store.ExternalProviderGmail, store.ExternalProviderGoogleCalendar:
		info.mail, info.err = reg.MailFor(ctx, acct.ID)
		if info.err != nil {
			info.err = fmt.Errorf("mail: %w", info.err)
			return info
		}
		info.calendar, _ = reg.CalendarFor(ctx, acct.ID)
		info.contacts, _ = reg.ContactsFor(ctx, acct.ID)
		info.tasks, _ = reg.TasksFor(ctx, acct.ID)
		info.mailbox, _ = reg.MailboxSettingsFor(ctx, acct.ID)
	case store.ExternalProviderIMAP:
		info.mail, info.err = reg.MailFor(ctx, acct.ID)
		if info.err != nil {
			info.err = fmt.Errorf("mail: %w", info.err)
		}
	case store.ExternalProviderExchangeEWS:
		info.mail, info.err = reg.MailFor(ctx, acct.ID)
		if info.err != nil {
			info.err = fmt.Errorf("mail: %w", info.err)
			return info
		}
		info.calendar, _ = reg.CalendarFor(ctx, acct.ID)
		info.contacts, _ = reg.ContactsFor(ctx, acct.ID)
		info.tasks, _ = reg.TasksFor(ctx, acct.ID)
		info.mailbox, _ = reg.MailboxSettingsFor(ctx, acct.ID)
	default:
		info.err = fmt.Errorf("unknown provider type")
	}
	return info
}

func printMatrix(results []providerInfo) {
	// Collect unique provider types for columns
	providerTypes := map[string]bool{}
	for _, r := range results {
		providerTypes[r.account.Provider] = true
	}

	providers := make([]string, 0, len(providerTypes))
	for p := range providerTypes {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	fmt.Println("| Capability | " + strings.Join(providers, " | ") + " |")
	fmt.Println("|---|" + strings.Join(providers, "|---") + "|")

	// Calendar capabilities
	printRow("Calendar: Provider", results, func(r providerInfo) string { return mark(r.calendar != nil) })
	printRow("Calendar: EventMutator", results, func(r providerInfo) string {
		_, ok := groupware.Supports[calendar.EventMutator](r.calendar)
		return mark(ok)
	})
	printRow("Calendar: InviteResponder", results, func(r providerInfo) string {
		_, ok := groupware.Supports[calendar.InviteResponder](r.calendar)
		return mark(ok)
	})
	printRow("Calendar: FreeBusyLooker", results, func(r providerInfo) string {
		_, ok := groupware.Supports[calendar.FreeBusyLooker](r.calendar)
		return mark(ok)
	})
	printRow("Calendar: ICSExporter", results, func(r providerInfo) string {
		_, ok := groupware.Supports[calendar.ICSExporter](r.calendar)
		return mark(ok)
	})
	printRow("Calendar: EventSearcher", results, func(r providerInfo) string {
		_, ok := groupware.Supports[calendar.EventSearcher](r.calendar)
		return mark(ok)
	})

	// Contacts capabilities
	printRow("Contacts: Provider", results, func(r providerInfo) string { return mark(r.contacts != nil) })
	printRow("Contacts: Searcher", results, func(r providerInfo) string {
		_, ok := groupware.Supports[contacts.Searcher](r.contacts)
		return mark(ok)
	})
	printRow("Contacts: Mutator", results, func(r providerInfo) string {
		_, ok := groupware.Supports[contacts.Mutator](r.contacts)
		return mark(ok)
	})
	printRow("Contacts: Grouper", results, func(r providerInfo) string {
		_, ok := groupware.Supports[contacts.Grouper](r.contacts)
		return mark(ok)
	})
	printRow("Contacts: PhotoFetcher", results, func(r providerInfo) string {
		_, ok := groupware.Supports[contacts.PhotoFetcher](r.contacts)
		return mark(ok)
	})

	// Tasks capabilities
	printRow("Tasks: Provider", results, func(r providerInfo) string { return mark(r.tasks != nil) })
	printRow("Tasks: Mutator", results, func(r providerInfo) string {
		_, ok := groupware.Supports[tasks.Mutator](r.tasks)
		return mark(ok)
	})
	printRow("Tasks: Completer", results, func(r providerInfo) string {
		_, ok := groupware.Supports[tasks.Completer](r.tasks)
		return mark(ok)
	})
	printRow("Tasks: ListManager", results, func(r providerInfo) string {
		_, ok := groupware.Supports[tasks.ListManager](r.tasks)
		return mark(ok)
	})

	// Mail capabilities
	printRow("Mail: EmailProvider", results, func(r providerInfo) string { return mark(r.mail != nil) })
	printRow("Mail: FlagMutator", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.FlagMutator](r.mail)
		return mark(ok)
	})
	printRow("Mail: CategoryMutator", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.CategoryMutator](r.mail)
		return mark(ok)
	})
	printRow("Mail: AttachmentProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.AttachmentProvider](r.mail)
		return mark(ok)
	})
	printRow("Mail: DraftProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.DraftProvider](r.mail)
		return mark(ok)
	})
	printRow("Mail: ExistingDraftSender", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.ExistingDraftSender](r.mail)
		return mark(ok)
	})
	printRow("Mail: ServerFilterProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.ServerFilterProvider](r.mail)
		return mark(ok)
	})
	printRow("Mail: NamedFolderProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.NamedFolderProvider](r.mail)
		return mark(ok)
	})
	printRow("Mail: NamedLabelProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.NamedLabelProvider](r.mail)
		return mark(ok)
	})
	printRow("Mail: MessageActionProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[email.MessageActionProvider](r.mail)
		return mark(ok)
	})

	// Mailbox settings capabilities
	printRow("Mailbox: OOFProvider", results, func(r providerInfo) string { return mark(r.mailbox != nil) })
	printRow("Mailbox: DelegationProvider", results, func(r providerInfo) string {
		_, ok := groupware.Supports[mailboxsettings.DelegationProvider](r.mailbox)
		return mark(ok)
	})
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func printRow(name string, results []providerInfo, fn func(providerInfo) string) {
	var cells []string
	for _, r := range results {
		cells = append(cells, fn(r))
	}
	fmt.Printf("| %s | %s |\n", name, strings.Join(cells, " | "))
}

func defaultStorePath() string {
	home := os.Getenv("HOME")
	if home == "" {
		return "sloptools.db"
	}
	return home + "/.config/sloptools/sloptools.db"
}

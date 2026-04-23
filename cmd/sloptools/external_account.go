package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/sloppy-org/sloptools/internal/store"
)

func cmdExternalAccount(args []string) int {
	if len(args) == 0 {
		printExternalAccountHelp()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdExternalAccountList(args[1:])
	case "add":
		return cmdExternalAccountAdd(args[1:])
	case "update":
		return cmdExternalAccountUpdate(args[1:])
	case "remove":
		return cmdExternalAccountRemove(args[1:])
	case "help", "-h", "--help":
		printExternalAccountHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown external-account subcommand: %s\n", args[0])
		printExternalAccountHelp()
		return 2
	}
}

func printExternalAccountHelp() {
	fmt.Println("sloptools external-account <list|add|update|remove> [flags]")
	fmt.Println()
	fmt.Println("common flags:")
	fmt.Println("  --data-dir PATH   sloppy data dir (default ~/.local/share/sloppy)")
	fmt.Println()
	fmt.Println("list flags:")
	fmt.Println("  --sphere work|private    filter by sphere")
	fmt.Println("  --provider NAME          filter by provider (gmail, exchange_ews, ...)")
	fmt.Println()
	fmt.Println("add flags:")
	fmt.Println("  --sphere work|private    required")
	fmt.Println("  --provider NAME          required (gmail, google_calendar, exchange_ews, imap, ics, todoist, evernote, bear, zotero, exchange)")
	fmt.Println("  --label TEXT             required account label (e.g. email address)")
	fmt.Println("  --config JSON            optional config object as JSON (default {})")
	fmt.Println("  --disabled               create in disabled state (default enabled)")
	fmt.Println()
	fmt.Println("update flags:")
	fmt.Println("  --id N                   required external account id")
	fmt.Println("  --sphere work|private    new sphere")
	fmt.Println("  --provider NAME          new provider")
	fmt.Println("  --label TEXT             new label")
	fmt.Println("  --config JSON            new config object as JSON")
	fmt.Println("  --enable / --disable     toggle enabled state")
	fmt.Println()
	fmt.Println("remove flags:")
	fmt.Println("  --id N                   required external account id")
}

func openStoreForExternalAccount(dataDir string) (*store.Store, error) {
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		dir = defaultDataDir()
	}
	return store.New(filepath.Join(dir, "sloppy.db"))
}

func printExternalAccountTable(accounts []store.ExternalAccount) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSPHERE\tPROVIDER\tLABEL\tENABLED\tCONFIG")
	for _, a := range accounts {
		enabled := "yes"
		if !a.Enabled {
			enabled = "no"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Sphere, a.Provider, a.AccountName, enabled, a.ConfigJSON)
	}
	_ = tw.Flush()
}

func cmdExternalAccountList(args []string) int {
	fs := flag.NewFlagSet("external-account list", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	sphere := fs.String("sphere", "", "filter by sphere (work|private)")
	provider := fs.String("provider", "", "filter by provider")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStoreForExternalAccount(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	var accounts []store.ExternalAccount
	switch {
	case strings.TrimSpace(*provider) != "":
		accounts, err = st.ListExternalAccountsByProvider(strings.TrimSpace(*provider))
	default:
		accounts, err = st.ListExternalAccounts(strings.TrimSpace(*sphere))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strings.TrimSpace(*provider) != "" && strings.TrimSpace(*sphere) != "" {
		filtered := make([]store.ExternalAccount, 0, len(accounts))
		want := strings.ToLower(strings.TrimSpace(*sphere))
		for _, a := range accounts {
			if strings.EqualFold(a.Sphere, want) {
				filtered = append(filtered, a)
			}
		}
		accounts = filtered
	}
	printExternalAccountTable(accounts)
	return 0
}

func cmdExternalAccountAdd(args []string) int {
	fs := flag.NewFlagSet("external-account add", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	sphere := fs.String("sphere", "", "work or private (required)")
	provider := fs.String("provider", "", "provider name (required)")
	label := fs.String("label", "", "account label (required)")
	configRaw := fs.String("config", "{}", "config JSON object")
	disabled := fs.Bool("disabled", false, "create in disabled state")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*label) == "" {
		fmt.Fprintln(os.Stderr, "--sphere, --provider, and --label are required")
		return 2
	}
	config, err := decodeConfigJSON(*configRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	st, err := openStoreForExternalAccount(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	account, err := st.CreateExternalAccount(*sphere, *provider, *label, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *disabled {
		enabled := false
		if err := st.UpdateExternalAccount(account.ID, store.ExternalAccountUpdate{Enabled: &enabled}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		account.Enabled = false
	}
	printExternalAccountTable([]store.ExternalAccount{account})
	return 0
}

func cmdExternalAccountUpdate(args []string) int {
	fs := flag.NewFlagSet("external-account update", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	id := fs.Int64("id", 0, "external account id (required)")
	sphere := fs.String("sphere", "", "new sphere")
	provider := fs.String("provider", "", "new provider")
	label := fs.String("label", "", "new label")
	configRaw := fs.String("config", "", "new config JSON object")
	enable := fs.Bool("enable", false, "enable account")
	disable := fs.Bool("disable", false, "disable account")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id <= 0 {
		fmt.Fprintln(os.Stderr, "--id is required")
		return 2
	}
	if *enable && *disable {
		fmt.Fprintln(os.Stderr, "--enable and --disable are mutually exclusive")
		return 2
	}
	update := store.ExternalAccountUpdate{}
	if v := strings.TrimSpace(*sphere); v != "" {
		update.Sphere = &v
	}
	if v := strings.TrimSpace(*provider); v != "" {
		update.Provider = &v
	}
	if fs.Lookup("label").Value.String() != "" {
		v := *label
		update.AccountName = &v
	}
	if *configRaw != "" {
		cfg, err := decodeConfigJSON(*configRaw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		update.Config = cfg
	}
	if *enable {
		b := true
		update.Enabled = &b
	}
	if *disable {
		b := false
		update.Enabled = &b
	}
	st, err := openStoreForExternalAccount(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	if err := st.UpdateExternalAccount(*id, update); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	account, err := st.GetExternalAccount(*id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printExternalAccountTable([]store.ExternalAccount{account})
	return 0
}

func cmdExternalAccountRemove(args []string) int {
	fs := flag.NewFlagSet("external-account remove", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "sloppy data dir")
	id := fs.Int64("id", 0, "external account id (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id <= 0 {
		fmt.Fprintln(os.Stderr, "--id is required")
		return 2
	}
	st, err := openStoreForExternalAccount(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	if err := st.DeleteExternalAccount(*id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("deleted external account %d\n", *id)
	return 0
}

func decodeConfigJSON(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("invalid --config JSON: %w", err)
	}
	if out == nil {
		return nil, errors.New("--config must be a JSON object")
	}
	return out, nil
}
